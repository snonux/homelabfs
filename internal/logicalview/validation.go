package logicalview

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

func (s *Store) validateStage(ctx context.Context, g Stage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l := s.config.Limits
	if s.closed || s.pending != nil || s.selected != s.visible {
		return fsapi.Busy
	}
	if len(s.claims) >= l.Claims || len(s.versions) >= l.Versions || len(s.identities) >= l.Identities && s.identities[g.Claim] == 0 {
		return fsapi.NoSpace
	}
	if !g.Claim.Valid() || len(g.Intent) == 0 || int64(len(g.Intent)) > l.MaxClaimBytes || len(g.Operations) > 64 || len(g.Dependencies) > l.Dependencies {
		return fsapi.Invalid
	}
	if s.identities[g.Claim] >= 2 {
		return fsapi.Stale
	}
	if g.Slot.Authority != s.hash || g.Slot.Sequencer != s.config.Authority.Sequencer || !sameCut(g.Previous, s.selected.cut) || !s.validOrigin(g.Origin) || g.OriginLaunch.Node != g.Origin.Origin || !g.OriginLaunch.Incarnation.Valid() {
		return fsapi.Stale
	}
	serial := uint64(1)
	if s.selected.cut != nil {
		if s.selected.cut.Slot.Serial == math.MaxUint64 {
			return fsapi.NoSpace
		}
		serial = s.selected.cut.Slot.Serial + 1
	}
	if g.Slot.Serial != serial {
		return fsapi.Stale
	}
	recipient := s.config.Authority.Sequencer
	if g.Origin.Origin == recipient.Node {
		recipient = s.config.Authority.OwnerPartner
	}
	if g.Recipient != recipient || g.Recipient.Node == g.Origin.Origin {
		return fsapi.Permission
	}
	total := int64(len(g.Intent))
	seen := map[record.NormalRef]bool{}
	newAllocations := map[fsapi.ObjectID]bool{}
	for _, d := range g.Dependencies {
		if seen[d.Ref] || int64(len(d.Bytes)) > l.MaxClaimBytes-total {
			return fsapi.NoSpace
		}
		total += int64(len(d.Bytes))
		seen[d.Ref] = true
	}
	for _, op := range g.Operations {
		if len(op.Objects) > int(l.MaxClaimBytes/512) || len(op.Entries) > int(l.MaxClaimBytes/512) || len(op.Pins) > int(l.MaxClaimBytes/256) || len(op.Participants) > 64 {
			return fsapi.NoSpace
		}
		if len(op.Objects) > l.Storage.Objects && l.Storage.Objects > 0 || len(op.Entries) > l.Storage.Entries && l.Storage.Entries > 0 || int64(len(op.Data)) > l.MaxClaimBytes-total {
			return fsapi.NoSpace
		}
		total += int64(len(op.Data)) + int64(len(op.Objects))*512 + int64(len(op.Entries))*512 + int64(len(op.Pins))*256
		for _, c := range op.Objects {
			if len(c.After.References) > s.config.Limits.Dependencies*64 || len(c.After.Text) > 4096 {
				return fsapi.NoSpace
			}
			total += int64(len(c.After.References))*512 + int64(len(c.After.Text))
		}
		if total > l.MaxClaimBytes {
			return fsapi.NoSpace
		}
		if op.ID != g.Claim {
			return fsapi.Stale
		}
		for _, c := range op.Objects {
			if c.Before != 0 {
				continue
			}
			id := c.After.Attr.Object
			if s.used[id] {
				if s.allocations[id] != g.Claim {
					return fsapi.Stale
				}
				continue
			}
			newAllocations[id] = true
			if len(newAllocations) > l.Identities-len(s.used) {
				return fsapi.NoSpace
			}
		}
		if op.Kind == storage.Reclaim {
			for _, h := range s.handles {
				if h.Object == op.Object {
					return fsapi.Busy
				}
			}
		}
	}
	return nil
}
func (s *Store) project(ctx context.Context, operations []storage.Operation) (*version, error) {
	next := &version{images: make(map[fsapi.UUID]storage.Snapshot, len(s.selected.images))}
	groups := map[fsapi.UUID][]storage.Operation{}
	for _, op := range operations {
		if _, ok := s.selected.images[op.Position.Volume]; !ok {
			return nil, fsapi.CrossDevice
		}
		groups[op.Position.Volume] = append(groups[op.Position.Volume], op)
	}
	// Count the complete closure before copying a baseline or allocating payloads.
	// The metadata pass borrows immutable input fields and uses the same primitive
	// validators as projection, including duplicates and multi-home fragments.
	var size int64
	for volume, before := range s.selected.images {
		n, err := storage.ProjectedBytes(ctx, before, groups[volume], s.config.Limits.Storage, s.config.Limits.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		if n > s.config.Limits.MaxSnapshotBytes-size {
			return nil, fsapi.NoSpace
		}
		size += n
	}
	for volume, before := range s.selected.images {
		// Unchanged images are immutable and safely shared between retained versions.
		image := before
		if ops := groups[volume]; len(ops) > 0 {
			var err error
			image, err = storage.Project(ctx, before, ops, s.config.Limits.Storage, s.config.Limits.MaxSnapshotBytes)
			if err != nil {
				return nil, err
			}
		}
		next.images[volume] = image
	}
	if err := validateClosure(next.images); err != nil {
		return nil, err
	}
	return next, nil
}
func (s *Store) validateEvidence(g Stage, e Evidence) error {
	if err := evidenceSize(e, s.config.Limits); err != nil {
		return err
	}
	if len(e.Proof) == 0 || len(e.Result) == 0 || len(e.Sources) == 0 || len(e.Sources) > s.config.Limits.Dependencies || len(e.Acceptances) == 0 || len(e.Acceptances) > s.config.Limits.Dependencies {
		return fsapi.Unavailable
	}
	if int64(len(e.Result))+int64(len(e.Proof)) > s.config.Limits.MaxClaimBytes {
		return fsapi.NoSpace
	}
	originDurable := false
	for _, source := range e.Sources {
		if len(source.Dependencies) > s.config.Limits.Dependencies {
			return fsapi.NoSpace
		}
		if !s.validOrigin(source.Stream) || source.Last.Stream != source.Stream || source.Last.Sequence != source.Through || source.Through == 0 || !source.Holder.Node.Valid() || !source.Holder.Incarnation.Valid() {
			return fsapi.Invalid
		}
		if source.Stream == g.Origin && source.Holder == g.OriginLaunch {
			originDurable = true
		}
	}
	if !originDurable {
		return fsapi.Unavailable
	}
	accepted := false
	for _, ref := range e.Acceptances {
		if ref.Creator == g.Recipient && ref.Stream.Origin == g.Recipient.Node && ref.Kind == record.NormalAcceptanceKind && ref.Sequence > 0 && ref.SHA256 != (record.Hash{}) {
			accepted = true
		}
	}
	if !accepted {
		return fsapi.Unavailable
	}
	// The external verifier additionally verifies exact COMMIT/result targets,
	// immutable graph bytes, synced prefixes and continuously bound authority.
	return nil
}
func (s *Store) verify(ctx context.Context, phase Phase, stage Stage, e Evidence) error {
	if err := evidenceSize(e, s.config.Limits); err != nil {
		return err
	}
	a, err := ownedAuthority(s.config.Authority)
	if err != nil {
		return err
	}
	return s.config.Verifier.Check(ctx, Check{Phase: phase, Authority: a, Stage: cloneStage(stage), Evidence: cloneEvidence(e), Domains: slices.Clone(s.config.Domains)})
}
func (s *Store) verifyBaseline(ctx context.Context, v *version) error {
	var baseline []storage.Snapshot
	for _, image := range v.images {
		copy, err := storage.Project(ctx, image, nil, s.config.Limits.Storage, s.config.Limits.MaxSnapshotBytes)
		if err != nil {
			return err
		}
		baseline = append(baseline, copy)
	}
	a, err := ownedAuthority(s.config.Authority)
	if err != nil {
		return err
	}
	return s.config.Verifier.Check(ctx, Check{Phase: Baseline, Authority: a, Baseline: baseline, Evidence: Evidence{Sources: cloneSources(s.config.Sources)}, Domains: slices.Clone(s.config.Domains)})
}
func (s *Store) hasVolume(volume fsapi.UUID, p fsapi.Position) bool {
	for _, v := range s.config.Authority.Volumes {
		if v.Volume == volume {
			return p.Volume == volume && p.Generation == v.Generation && p.Configuration == v.Configuration && p.Epoch == v.Epoch
		}
	}
	return false
}
func (s *Store) validOrigin(o record.OriginStream) bool {
	return o.Cluster == s.config.Authority.Cluster && o.Journal.Valid() && slices.Contains(s.config.Authority.Admitted, o.Origin) && s.hasVolume(o.Volume, fsapi.Position{Volume: o.Volume, Generation: o.Generation, Configuration: o.Configuration, Epoch: o.Epoch})
}
func (s *Store) retainedCut(c record.SelectedRef) bool {
	for _, v := range s.versions {
		if v.cut != nil && *v.cut == c {
			return true
		}
	}
	return false
}
func sameDomains(a, b []Domain) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[fsapi.UUID]bool{}
	for _, d := range a {
		if !d.ID.Valid() || !d.Session.Valid() || seen[d.ID] || !slices.Contains(b, d) {
			return false
		}
		seen[d.ID] = true
	}
	return true
}
func mergeSources(old, added []record.SourceCoverage) []record.SourceCoverage {
	out := cloneSources(old)
	for _, s := range added {
		found := false
		for i, v := range out {
			if v.Stream == s.Stream && v.Holder == s.Holder {
				if s.Through > v.Through {
					out[i] = s
				}
				found = true
				break
			}
		}
		if !found {
			out = append(out, s)
		}
	}
	return cloneSources(out)
}

func validateClosure(images map[fsapi.UUID]storage.Snapshot) error {
	objects := map[fsapi.ObjectID]storage.Object{}
	entries := map[storage.Reference]fsapi.ObjectID{}
	for _, s := range images {
		for _, o := range s.Image.Objects {
			objects[o.Attr.Object] = o
		}
		for _, e := range s.Image.Entries {
			if e.Target != nil {
				entries[storage.Reference{Parent: e.Parent, Name: e.Name, Revision: e.Revision}] = *e.Target
			}
		}
	}
	for ref, target := range entries {
		object, found := objects[target]
		if !found || !slices.Contains(object.References, ref) {
			return fmt.Errorf("%w: missing exact cross-home object/reference", fsapi.Stale)
		}
	}
	for id, o := range objects {
		for _, ref := range o.References {
			target, found := entries[ref]
			if !found || target != id {
				return fmt.Errorf("%w: missing exact cross-home entry", fsapi.Stale)
			}
		}
		if o.Attr.Kind != fsapi.Directory {
			continue
		}
		if o.Parent == nil {
			return fsapi.Invalid
		}
		visited := map[fsapi.ObjectID]bool{}
		cur := id
		for {
			if visited[cur] {
				return fsapi.Invalid
			}
			visited[cur] = true
			ancestor, found := objects[cur]
			if !found || ancestor.Attr.Kind != fsapi.Directory || ancestor.Parent == nil {
				return fsapi.Stale
			}
			if ancestor.Parent.Object == cur {
				if cur != images[cur.Home].Image.Root {
					return fsapi.Invalid
				}
				break
			}
			cur = ancestor.Parent.Object
		}
	}
	return nil
}

func evidenceSize(e Evidence, l Limits) error {
	if len(e.Sources) > l.Dependencies || len(e.Acceptances) > l.Dependencies || len(e.Acks) > l.Dependencies || len(e.Claims) > l.Claims {
		return fsapi.NoSpace
	}
	cost := int64(len(e.Acceptances))*256 + int64(len(e.Acks))*64 + int64(len(e.Claims))*32
	add := func(n int64) bool {
		if n > l.MaxClaimBytes-cost {
			return false
		}
		cost += n
		return true
	}
	if cost > l.MaxClaimBytes || !add(int64(len(e.Proof))) || !add(int64(len(e.Result))) {
		return fsapi.NoSpace
	}
	for _, source := range e.Sources {
		if len(source.Dependencies) > l.Dependencies || !add(512+int64(len(source.Dependencies))*256) {
			return fsapi.NoSpace
		}
	}
	return nil
}
