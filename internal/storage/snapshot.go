package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Snapshot owns a clean catalog and independent complete regular-file payloads,
// in Image.Payloads order. It is a bounded local copy, not retention authority.
type Snapshot struct {
	Image Image
	Data  [][]byte
}

// Capture copies a consistent installed cut. The caller excludes all Store use
// until return, and must preserve the returned bytes immutably before advancing
// retention. maxBytes bounds the sum of logical payload sizes, including holes.
func (s *Store) Capture(ctx context.Context, maxBytes int64) (Snapshot, error) {
	image, err := s.Export(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Snapshot{Image: image}
	var total int64
	if maxBytes < 1 {
		return Snapshot{}, fsapi.Invalid
	}
	for _, p := range image.Payloads {
		if p.Size > maxBytes-total {
			return Snapshot{}, fsapi.NoSpace
		}
		total += p.Size
		data, e := s.copyPayload(ctx, p)
		if e != nil {
			return Snapshot{}, e
		}
		out.Data = append(out.Data, data)
	}
	return out, nil
}

// Restore copies a validated immutable snapshot into two new empty private roots.
// Later payload writes cannot modify the source snapshot. Interrupted roots are
// preserved and must never be reused as a fresh recovery destination.
func Restore(ctx context.Context, data, journal Filesystem, config Config, snapshot Snapshot, maxBytes int64) (*Store, error) {
	if err := ValidateSnapshot(snapshot, config.Limits, maxBytes); err != nil {
		return nil, err
	}
	s, err := newStore(data, journal, config)
	if err != nil {
		return nil, err
	}
	if snapshot.Image.Volume != config.Volume || snapshot.Image.Root != config.Root {
		return nil, fsapi.Invalid
	}
	if err = s.validateRoots(ctx, true); err != nil {
		return nil, err
	}
	if err = s.initialize(ctx); err != nil {
		return nil, err
	}
	for _, o := range snapshot.Image.Objects {
		if err = s.ensureObject(ctx, o.Descriptor, false); err != nil {
			return nil, err
		}
	}
	for _, d := range snapshot.Image.Retired {
		if err = s.ensureObject(ctx, d, false); err != nil {
			return nil, err
		}
		if d.Kind == fsapi.Regular {
			if err = data.Unlink(ctx, objectPath(d.Object)+"/payload"); err != nil {
				return nil, err
			}
			if err = data.SyncDir(ctx, objectPath(d.Object)); err != nil {
				return nil, err
			}
		}
	}
	for n, p := range snapshot.Image.Payloads {
		if err = s.restorePayload(ctx, p, snapshot.Data[n]); err != nil {
			return nil, err
		}
	}
	if err = s.loadImage(ctx, snapshot.Image); err != nil {
		return nil, err
	}
	s.ready = true
	return s, nil
}

// ValidateSnapshot checks catalog, exact payload order, sizes and hashes before
// any destination is changed. A durable catalog without matching bytes fails.
func ValidateSnapshot(s Snapshot, limits Limits, maxBytes int64) error {
	if maxBytes < 1 || len(s.Data) != len(s.Image.Payloads) {
		return fsapi.Invalid
	}
	if err := validateImage(s.Image, limits); err != nil {
		return err
	}
	var total int64
	for i, p := range s.Image.Payloads {
		if p.Size > maxBytes-total || int64(len(s.Data[i])) != p.Size {
			return fsapi.NoSpace
		}
		total += p.Size
		if sha256.Sum256(s.Data[i]) != p.SHA256 {
			return fmt.Errorf("%w: immutable payload hash", fsapi.IO)
		}
	}
	return nil
}

func (s *Store) copyPayload(ctx context.Context, p Payload) ([]byte, error) {
	f, err := s.data.Open(ctx, objectPath(p.Object)+"/payload", os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw := make([]byte, p.Size)
	if len(raw) > 0 {
		if _, err = f.ReadAt(ctx, raw, 0); err != nil {
			return nil, err
		}
	}
	if sha256.Sum256(raw) != p.SHA256 {
		return nil, fsapi.Stale
	}
	return raw, nil
}

func (s *Store) restorePayload(ctx context.Context, p Payload, raw []byte) error {
	f, err := s.data.Open(ctx, objectPath(p.Object)+"/payload", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	n, err := f.WriteAt(ctx, raw, 0)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	return f.Sync(ctx)
}
