package fsservice

import (
	"context"
	"crypto/sha256"
	"io"

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

// VerifyControlledSource checks a live sealed capture and rereads its complete
// original journal prefix through the service's admitted backing OS root. It
// never enters the service worker or touches a FUSE path, and can therefore run
// from a controller Facts callback. The caller retains the source slot and
// immutable State throughout this call. Reopened/unsealed sources refuse.
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
	reader, err := s.reader(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	prefix := sha256.New()
	for {
		entry, err := reader.Next(ctx)
		if err != nil {
			if err == io.EOF {
				return fsapi.Stale
			}
			return err
		}
		if entry.Record.Header.Incarnation != info.Incarnation || record.Hash(sha256.Sum256(entry.Bytes)) != entry.Reference.SHA256 {
			return fsapi.Stale
		}
		_, _ = prefix.Write(entry.Bytes)
		if entry.Reference.Position == source.Through.Position {
			if entry.Reference != seal.through || [32]byte(prefix.Sum(nil)) != seal.prefix || !s.sourceServing() {
				return fsapi.Stale
			}
			return nil
		}
	}
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
