package fsservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

func (s *Service) checkRoots(ctx context.Context) error {
	roots := []interface {
		Stat(context.Context, string) (vfs.Info, error)
	}{s.config.Source, s.config.Reservations, s.config.Workspace.Data, s.config.Workspace.Journal, s.config.Workspace.Progress}
	identities := make(map[vfs.Identity]bool)
	for _, root := range roots {
		if root == nil {
			return fsapi.Invalid
		}
		info, err := root.Stat(ctx, ".")
		if err != nil {
			return err
		}
		if info.Mode != fs.ModeDir|0o700 || info.UID != s.config.Storage.OwnerUID || identities[info.Identity] {
			return fsapi.Permission
		}
		identities[info.Identity] = true
	}
	return nil
}
func (s *Service) scanReservations(ctx context.Context) error {
	f := s.config.Reservations
	dir, err := f.OpenDir(ctx, ".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	root, err := dir.Stat(ctx)
	if err != nil {
		return err
	}
	count := 0
	for {
		page, err := dir.ReadDir(ctx, 64)
		if err != nil && err != io.EOF {
			return err
		}
		for _, e := range page {
			count++
			if count > s.config.Limits.Reservations {
				return fsapi.NoSpace
			}
			if !strings.HasPrefix(e.Name, "allocation-") || !strings.HasSuffix(e.Name, ".hfr") || e.Info.Mode != 0o600 || e.Info.UID != s.config.Storage.OwnerUID || e.Info.Links != 1 || e.Info.Identity.Device != root.Identity.Device || e.Info.Size != 104 {
				return fsapi.Invalid
			}
			file, eerr := f.Open(ctx, e.Name, os.O_RDONLY, 0)
			if eerr != nil {
				return eerr
			}
			raw := make([]byte, 104)
			n, eerr := file.ReadAt(ctx, raw, 0)
			closeErr := file.Close()
			if eerr != nil {
				return eerr
			}
			if closeErr != nil {
				return closeErr
			}
			if n != len(raw) {
				return io.ErrUnexpectedEOF
			}
			hash := sha256.Sum256(raw[:72])
			if !bytes.Equal(raw[:8], []byte("HFSRSV01")) || !bytes.Equal(raw[72:], hash[:]) {
				return fsapi.Invalid
			}
			var id fsapi.ObjectID
			var op fsapi.UUID
			copy(id.Home[:], raw[8:24])
			copy(id.Allocation[:], raw[24:40])
			copy(op[:], raw[40:56])
			if !bytes.Equal(raw[56:72], make([]byte, 16)) || id.Home != s.config.Storage.Volume || !id.Valid() || !op.Valid() || e.Name != "allocation-"+id.Allocation.String()+".hfr" || s.allocations[id] || s.ids[op] {
				return fsapi.Stale
			}
			s.allocations[id] = true
			s.ids[op] = true
		}
		if err == io.EOF {
			break
		}
	}
	return nil
}
func (s *Service) newUUID() (fsapi.UUID, error) {
	if len(s.ids) >= s.config.Apply.MaxFrames+s.config.Limits.Reservations*2+s.config.Limits.Handles*2 {
		return fsapi.UUID{}, fsapi.NoSpace
	}
	for range 128 {
		var id fsapi.UUID
		if _, err := io.ReadFull(s.config.Random, id[:]); err != nil {
			return id, err
		}
		id[6] = (id[6] & 15) | 64
		id[8] = (id[8] & 63) | 128
		if !s.ids[id] {
			s.ids[id] = true
			return id, nil
		}
	}
	return fsapi.UUID{}, fmt.Errorf("%w: UUID collision budget", fsapi.NoSpace)
}
func (s *Service) reserve(ctx context.Context, op fsapi.UUID) (fsapi.ObjectID, error) {
	if len(s.allocations) >= s.config.Limits.Reservations {
		return fsapi.ObjectID{}, fsapi.NoSpace
	}
	for range 128 {
		var allocation fsapi.UUID
		if _, err := io.ReadFull(s.config.Random, allocation[:]); err != nil {
			return fsapi.ObjectID{}, err
		}
		allocation[6] = (allocation[6] & 15) | 64
		allocation[8] = (allocation[8] & 63) | 128
		id := fsapi.ObjectID{Home: s.config.Storage.Volume, Allocation: allocation}
		if s.allocations[id] {
			continue
		}
		// Reserve in memory before the first fallible write; any uncertain failure
		// blocks the service and preserves the root for subsequent admission checks.
		s.allocations[id] = true
		raw := append([]byte("HFSRSV01"), id.Home[:]...)
		raw = append(raw, id.Allocation[:]...)
		raw = append(raw, op[:]...)
		raw = append(raw, make([]byte, 16)...)
		sum := sha256.Sum256(raw)
		raw = append(raw, sum[:]...)
		name := "allocation-" + id.Allocation.String() + ".hfr"
		f, err := s.config.Reservations.Open(ctx, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fsapi.ObjectID{}, s.block(err)
		}
		n, err := f.WriteAt(ctx, raw, 0)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
		if err == nil {
			err = f.Sync(ctx)
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = s.config.Reservations.SyncDir(ctx, ".")
		}
		if err != nil {
			return fsapi.ObjectID{}, s.block(err)
		}
		return id, nil
	}
	return fsapi.ObjectID{}, fsapi.NoSpace
}
