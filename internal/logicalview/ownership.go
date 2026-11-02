package logicalview

import (
	"crypto/sha256"
	"encoding/json"
	"slices"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

func cloneStage(s Stage) Stage {
	s.Previous = cloneCut(s.Previous)
	s.Intent = slices.Clone(s.Intent)
	s.Operations = slices.Clone(s.Operations)
	for i := range s.Operations {
		s.Operations[i] = storage.CopyOperation(s.Operations[i])
	}
	s.Dependencies = slices.Clone(s.Dependencies)
	for i := range s.Dependencies {
		s.Dependencies[i].Bytes = slices.Clone(s.Dependencies[i].Bytes)
	}
	return s
}
func cloneSources(sources []record.SourceCoverage) []record.SourceCoverage {
	out := slices.Clone(sources)
	for i := range out {
		out[i].Dependencies = slices.Clone(out[i].Dependencies)
	}
	return out
}
func cloneEvidence(e Evidence) Evidence {
	e.Claims = slices.Clone(e.Claims)
	e.Sources = cloneSources(e.Sources)
	e.Acceptances = slices.Clone(e.Acceptances)
	e.Result = slices.Clone(e.Result)
	e.Proof = slices.Clone(e.Proof)
	e.Acks = slices.Clone(e.Acks)
	return e
}
func cloneClaim(c Claim) Claim {
	c.Stage = cloneStage(c.Stage)
	if c.Evidence != nil {
		e := cloneEvidence(*c.Evidence)
		c.Evidence = &e
	}
	return c
}
func cloneCut(c *record.SelectedRef) *record.SelectedRef {
	if c == nil {
		return nil
	}
	v := *c
	return &v
}
func sameCut(a, b *record.SelectedRef) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func cloneFrontiers(f Frontiers) Frontiers {
	f.Sources = slices.Clone(f.Sources)
	f.Selected = cloneCut(f.Selected)
	f.Visible = cloneCut(f.Visible)
	f.Installed = cloneCut(f.Installed)
	f.Durable = cloneCut(f.Durable)
	return f
}
func ownedAuthority(a record.NormalAuthority) (record.NormalAuthority, error) {
	raw, err := record.EncodeNormalAuthority(a)
	if err != nil {
		return a, err
	}
	return record.DecodeNormalAuthority(raw)
}
func digestStage(stage Stage) (record.Hash, int64, error) {
	raw, err := json.Marshal(stage)
	return sha256.Sum256(raw), int64(len(raw)), err
}
func snapshotBytes(s storage.Snapshot, limits storage.Limits, maxBytes int64) (int64, error) {
	return storage.SnapshotBytes(s, limits, maxBytes)
}
func sourceCovers(sources []record.SourceCoverage, ref record.NormalRef) bool {
	for _, s := range sources {
		if s.Stream == ref.Stream && s.Through >= ref.Sequence && s.Last.Stream == s.Stream && s.Last.Sequence == s.Through && (s.Through != ref.Sequence || s.Last == ref) {
			return true
		}
	}
	return false
}
func validCut(c record.SelectedRef) bool {
	return c.Slot.Serial > 0 && c.Slot.Authority != (record.Hash{}) && c.Slot.Sequencer.Node.Valid() && c.Slot.Sequencer.Incarnation.Valid() && c.Certificate.Kind == record.NormalSelectionKind && c.Certificate.Creator == c.Slot.Sequencer && c.Certificate.Sequence > 0 && c.Certificate.SHA256 != (record.Hash{})
}
