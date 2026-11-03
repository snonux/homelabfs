package fsservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"github.com/snonux/homelabfs/internal/storage"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

// ControlledViewSource owns an exact sealed applied state and durable journal
// frame. It is private local evidence, never a normal publication certificate.
type ControlledViewSource struct {
	Journal    ControlledJournal
	Through    record.FrameRef
	State      []byte
	Visibility fsapi.UUID
	Applied    fsapi.Position
}

// ViewSource captures complete metadata and payload bytes on the service worker.
// The original stream is flushed and reverified, including complete frame bytes.
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
	image, err := s.applied.Store.Export(ctx)
	if err != nil {
		return ControlledViewSource{}, err
	}
	catalog, err := storage.EncodeImage(image, s.config.Storage.Limits)
	if err != nil {
		return ControlledViewSource{}, err
	}
	total := int64(8 + len(catalog))
	for _, p := range image.Payloads {
		if p.Size < 0 || total > maxBytes-8 || p.Size > maxBytes-total-8 {
			return ControlledViewSource{}, fsapi.NoSpace
		}
		total += 8 + p.Size
	}
	if total > maxBytes {
		return ControlledViewSource{}, fsapi.NoSpace
	}
	snapshot, err := s.applied.Store.Capture(ctx, maxBytes)
	if err != nil {
		return ControlledViewSource{}, err
	}
	var buffer bytes.Buffer
	buffer.Grow(int(total))
	_ = binary.Write(&buffer, binary.BigEndian, uint64(len(catalog)))
	_, _ = buffer.Write(catalog)
	for _, data := range snapshot.Data {
		_ = binary.Write(&buffer, binary.BigEndian, uint64(len(data)))
		_, _ = buffer.Write(data)
	}
	state := buffer.Bytes()
	reader, err := s.reader(ctx)
	if err != nil {
		return ControlledViewSource{}, err
	}
	defer func() { _ = reader.Close() }()
	var through record.FrameRef
	var incarnation fsapi.UUID
	for {
		frame, e := reader.Next(ctx)
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return ControlledViewSource{}, e
		}
		if record.Hash(sha256.Sum256(frame.Bytes)) != frame.Reference.SHA256 {
			return ControlledViewSource{}, fsapi.IO
		}
		through = frame.Reference
		incarnation = frame.Record.Header.Incarnation
	}
	if incarnation != info.Incarnation || through.Position.Sequence == 0 || through.Position.Volume != info.Stream.Volume || through.Position.Generation != info.Stream.Generation || through.Position.Configuration != info.Stream.Configuration || through.Position.Epoch != info.Stream.Epoch {
		return ControlledViewSource{}, fsapi.Unavailable
	}
	return ControlledViewSource{Journal: info, Through: through, State: state, Visibility: s.config.Workspace.ID, Applied: image.Position}, nil
}

// ControlledViewMetadataBytes returns the immutable admitted catalog bound used
// to reserve peak source capture memory before any snapshot/copy is allocated.
func (s *Service) ControlledViewMetadataBytes() int64 {
	if s.config.Storage.Limits.FileBytes == 0 {
		return 16 << 20
	}
	return s.config.Storage.Limits.FileBytes
}

// VerifyViewReference rereads the actual complete retained source, checking the
// protected full frame identity. It performs no hash-only source substitution.
func (c *ControlledCut) VerifyViewReference(ctx context.Context, ref record.FrameRef) error {
	if !c.active || c.service.State() != Serving {
		return fsapi.Unavailable
	}
	reader, err := c.service.reader(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	for {
		entry, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fsapi.Stale
			}
			return err
		}
		if entry.Reference.Position == ref.Position {
			if entry.Record.Header.Incarnation != c.service.config.Authority.OwnerIncarnation || entry.Reference != ref || record.Hash(sha256.Sum256(entry.Bytes)) != ref.SHA256 {
				return fsapi.Stale
			}
			return nil
		}
	}
}
