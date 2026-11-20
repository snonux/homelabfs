package fsservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"

	"github.com/snonux/homelabfs/internal/storage"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

// ControlledViewSource owns an exact sealed applied state and durable journal
// frame. It is private local evidence, never a normal publication certificate.
type ControlledViewSource struct {
	seal       *sourceSeal
	Journal    ControlledJournal
	Through    record.FrameRef
	State      []byte
	Visibility fsapi.UUID
	Applied    fsapi.Position
}

// ViewSource captures complete metadata and payload bytes on the service worker.
// The original stream is flushed and reverified, including complete frame bytes
// (incrementally since te4, see journal_index.go).
// It never returns invented position-zero evidence. maxBytes bounds payloads and
// the serialized snapshot. Applied names the actual installed storage prefix;
// Through names the flushed stream tail, which may include non-state structural
// or lifetime records. No tail record is inferred to be an applied mutation.
// Structural/canonical metadata is separately bounded
// by the service storage limits before capture.
func (c *ControlledCut) ViewSource(ctx context.Context, maxBytes int64) (ControlledViewSource, error) {
	if !c.active || maxBytes < 1 {
		return ControlledViewSource{}, fsapi.Permission
	}
	s := c.service
	s.sourceCut.Lock()
	defer s.sourceCut.Unlock()
	if s.State() != Serving {
		return ControlledViewSource{}, fsapi.Unavailable
	}
	info, err := s.ControlledJournalInfo(ctx, *s.config.Validation)
	if err != nil {
		return ControlledViewSource{}, err
	}
	if err = s.writer.Flush(ctx); err != nil {
		return ControlledViewSource{}, s.block(err)
	}
	state, applied, err := s.captureState(ctx, maxBytes)
	if err != nil {
		return ControlledViewSource{}, err
	}
	through, prefix, err := s.journalTail(ctx, info)
	if err != nil {
		return ControlledViewSource{}, err
	}
	seal := &sourceSeal{owner: s, journal: info, through: through, visibility: s.config.Workspace.ID, applied: applied, state: sha256.Sum256(state), prefix: prefix}
	return ControlledViewSource{seal: seal, Journal: info, Through: through, State: state, Visibility: s.config.Workspace.ID, Applied: applied}, nil
}

// ControlledViewMetadataBytes returns the immutable admitted catalog bound used
// to reserve peak source capture memory before any snapshot/copy is allocated.
func (s *Service) ControlledViewMetadataBytes() int64 {
	if s.config.Storage.Limits.FileBytes == 0 {
		return 16 << 20
	}
	return s.config.Storage.Limits.FileBytes
}

// captureState serializes the applied catalog and every payload as the
// source State (length-prefixed catalog, then each payload), refusing with
// NoSpace before allocation when they exceed maxBytes. It returns the
// applied storage position the bytes describe.
func (s *Service) captureState(ctx context.Context, maxBytes int64) ([]byte, fsapi.Position, error) {
	image, err := s.applied.Store.Export(ctx)
	if err != nil {
		return nil, fsapi.Position{}, err
	}
	catalog, err := storage.EncodeImage(image, s.config.Storage.Limits)
	if err != nil {
		return nil, fsapi.Position{}, err
	}
	total := int64(8 + len(catalog))
	for _, p := range image.Payloads {
		if p.Size < 0 || total > maxBytes-8 || p.Size > maxBytes-total-8 {
			return nil, fsapi.Position{}, fsapi.NoSpace
		}
		total += 8 + p.Size
	}
	if total > maxBytes {
		return nil, fsapi.Position{}, fsapi.NoSpace
	}
	snapshot, err := s.applied.Store.Capture(ctx, maxBytes)
	if err != nil {
		return nil, fsapi.Position{}, err
	}
	var buffer bytes.Buffer
	buffer.Grow(int(total))
	_ = binary.Write(&buffer, binary.BigEndian, uint64(len(catalog)))
	_, _ = buffer.Write(catalog)
	for _, data := range snapshot.Data {
		_ = binary.Write(&buffer, binary.BigEndian, uint64(len(data)))
		_, _ = buffer.Write(data)
	}
	return buffer.Bytes(), image.Position, nil
}

// journalTail verifies the flushed journal (journalFrame) and returns its
// last frame and the cumulative digest of every frame. Every frame must hash
// to its reference (else IO); the tail must carry the journal incarnation and
// lie in the exact stream position, never position zero (else Unavailable).
func (s *Service) journalTail(ctx context.Context, info ControlledJournal) (record.FrameRef, [32]byte, error) {
	last, err := s.journalFrame(ctx, nil)
	if err != nil {
		return record.FrameRef{}, [32]byte{}, err
	}
	if last.live && !last.clean {
		return record.FrameRef{}, [32]byte{}, fsapi.IO
	}
	through := last.ref
	if !last.live || last.incarnation != info.Incarnation || through.Position.Sequence == 0 || through.Position.Volume != info.Stream.Volume || through.Position.Generation != info.Stream.Generation || through.Position.Configuration != info.Stream.Configuration || through.Position.Epoch != info.Stream.Epoch {
		return record.FrameRef{}, [32]byte{}, fsapi.Unavailable
	}
	return through, last.prefix, nil
}

// VerifyViewReference reverifies the actual complete retained source
// (journalFrame: verified bytes rehashed, appended frames decoded, any
// change reruns the complete verification) and checks the protected full
// frame identity at ref's position. It performs no hash-only source
// substitution.
func (c *ControlledCut) VerifyViewReference(ctx context.Context, ref record.FrameRef) error {
	if !c.active || c.service.State() != Serving {
		return fsapi.Unavailable
	}
	f, err := c.service.journalFrame(ctx, &ref.Position)
	if err != nil {
		return err
	}
	if !f.live || f.incarnation != c.service.config.Authority.OwnerIncarnation || f.ref != ref || !f.hashOK {
		return fsapi.Stale
	}
	return nil
}
