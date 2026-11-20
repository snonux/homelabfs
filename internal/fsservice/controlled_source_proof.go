package fsservice

import (
	"context"
	"crypto/sha256"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

// sourceSeal is created only while the service worker captures actual applied
// catalog/payload bytes. It is deliberately absent from serialized snapshots:
// another service lifetime must reconstruct correspondence, not import a hash.
type sourceSeal struct {
	owner         *Service
	journal       ControlledJournal
	through       record.FrameRef
	visibility    fsapi.UUID
	applied       fsapi.Position
	state, prefix [32]byte
}

// VerifyControlledSource checks a live sealed capture against its complete
// original journal prefix through the service's admitted backing OS root. It
// never enters the service worker or touches a FUSE path, and can therefore run
// from a controller Facts callback. The caller retains the source slot and
// immutable State throughout this call. Reopened/unsealed sources refuse.
//
// The journal is reverified on every call (refreshJournal: verified bytes are
// rehashed, appended frames decoded; any change reruns the complete
// verification), and then every frame through the source's position must
// carry the journal incarnation and hash to its reference, and the
// cumulative digest of those frames must equal the sealed prefix digest:
// the same conditions the former per-call full reread checked frame by frame.
func (s *Service) VerifyControlledSource(ctx context.Context, profile ValidationProfile, source ControlledViewSource) error {
	seal := source.seal
	if seal == nil || seal.owner != s || !s.sourceServing() || source.Journal != seal.journal || source.Through != seal.through || source.Visibility != seal.visibility || source.Applied != seal.applied || sha256.Sum256(source.State) != seal.state {
		return fsapi.Stale
	}
	if !s.sourceCut.TryRLock() {
		return fsapi.WouldBlock
	}
	defer s.sourceCut.RUnlock()
	info, err := s.ControlledJournalInfo(ctx, profile)
	if err != nil || info != seal.journal || s.config.Workspace.ID != seal.visibility {
		return fsapi.Stale
	}
	view, err := s.refreshJournal(ctx)
	if err != nil {
		return err
	}
	i := view.find(source.Through.Position)
	if i < 0 || i >= view.goodHash || i >= view.uniform || view.frames[0].incarnation != info.Incarnation {
		return fsapi.Stale
	}
	if view.frames[i].ref != seal.through || view.frames[i].prefix != seal.prefix || !s.sourceServing() {
		return fsapi.Stale
	}
	return nil
}

func (s *Service) flushSource(ctx context.Context) error {
	s.sourceCut.Lock()
	defer s.sourceCut.Unlock()
	return s.writer.Flush(ctx)
}

func (s *Service) sourceServing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.State() == Serving
}
