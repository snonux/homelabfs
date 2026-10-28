package storage

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

func scanDir(ctx context.Context, f Filesystem, p string, limit int) ([]vfs.Entry, error) {
	d, err := f.OpenDir(ctx, p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	var all []vfs.Entry
	for {
		page, e := d.ReadDir(ctx, min(vfs.MaxReadDir, limit-len(all)+1))
		all = append(all, page...)
		if len(all) > limit {
			return nil, fsapi.NoSpace
		}
		if e == io.EOF {
			return all, nil
		}
		if e != nil {
			return nil, e
		}
	}
}

func (s *Store) validateTree(ctx context.Context) error {
	for _, r := range []struct {
		f    Filesystem
		role string
	}{{s.data, "data"}, {s.journal, "journal"}} {
		root, err := r.f.Stat(ctx, ".")
		if err != nil {
			return err
		}
		seen := map[vfs.Identity]bool{root.Identity: true}
		budget := s.config.Limits.Entries + s.config.Limits.Objects*5 + s.config.Limits.Operations*4 + 32
		if err = s.walkPrivate(ctx, r.f, ".", r.role, root.Identity.Device, seen, &budget); err != nil {
			return err
		}
	}
	pending, err := s.reservations(ctx)
	if err != nil {
		return err
	}
	for op, id := range pending {
		if o, ok := s.objects[id]; ok && o.Descriptor.Creation != op {
			return fsapi.Invalid
		}
		if d, ok := s.retired[id]; ok && d.Creation != op {
			return fsapi.Invalid
		}
	}
	return nil
}
func (s *Store) walkPrivate(ctx context.Context, f Filesystem, p, role string, device uint64, seen map[vfs.Identity]bool, budget *int) error {
	items, err := scanDir(ctx, f, p, *budget)
	if err != nil {
		return err
	}
	parts := strings.Split(p, "/")
	if role == "data" && len(parts) == 3 && parts[0] == "objects" {
		raw, e := readPrivate(ctx, f, p+"/identity.json", s.config.OwnerUID, s.config.Limits.FileBytes)
		if e != nil {
			return e
		}
		var w descriptorWire
		if e = strictJSON(raw, &w); e != nil {
			return e
		}
		d, e := fromDescriptor(w)
		if e != nil {
			return e
		}
		if d.Object.Home != s.config.Volume || objectPath(d.Object) != p {
			return fsapi.Invalid
		}
		for _, item := range items {
			if item.Name == "payload" && d.Kind != fsapi.Regular {
				return fmt.Errorf("%w: nonregular object has payload", fsapi.Invalid)
			}
		}
	}

	for _, e := range items {
		*budget--
		if *budget < 0 {
			return fsapi.NoSpace
		}
		full := path.Join(p, e.Name)
		if seen[e.Info.Identity] || e.Info.Identity.Device != device || e.Info.UID != s.config.OwnerUID {
			return fmt.Errorf("%w: physical alias/mount/owner %s", fsapi.Invalid, full)
		}
		seen[e.Info.Identity] = true
		if e.Info.Mode.IsDir() {
			if e.Info.Mode != 0o700|fs.ModeDir {
				return fsapi.Invalid
			}
			if !allowedDir(full, role) {
				return fmt.Errorf("%w: unexpected directory %s", fsapi.Invalid, full)
			}
			if err = s.walkPrivate(ctx, f, full, role, device, seen, budget); err != nil {
				return err
			}
		} else {
			if e.Info.Mode != 0o600 || e.Info.Links != 1 || !allowedFile(full, role) {
				return fmt.Errorf("%w: unexpected/private file %s", fsapi.Invalid, full)
			}
		}
	}
	return nil
}
func allowedDir(p, role string) bool {
	parts := strings.Split(p, "/")
	if len(parts) == 1 {
		return p == "checkpoints" || p == "staging" || role == "data" && p == "objects" || role == "journal" && p == "segments"
	}
	if parts[0] == "staging" || parts[0] == "checkpoints" {
		return len(parts) == 2 && uuidName(parts[1])
	}
	if role == "data" && parts[0] == "objects" {
		if len(parts) == 2 {
			return shardName(parts[1])
		}
		return len(parts) == 3 && shardName(parts[1]) && uuidName(parts[2]) && parts[2][:2] == parts[1]
	}
	return false
}
func allowedFile(p, role string) bool {
	parts := strings.Split(p, "/")
	if len(parts) == 1 {
		return p == "format.json" || role == "data" && (p == "CURRENT" || strings.HasPrefix(p, "CURRENT.tmp-") && uuidName(strings.TrimPrefix(p, "CURRENT.tmp-")))
	}
	if parts[0] == "staging" && len(parts) == 3 {
		return uuidName(parts[1]) && (parts[2] == "allocation.json" || parts[2] == "catalog.json" || parts[2] == "manifest.json")
	}
	if parts[0] == "checkpoints" {
		if role == "data" {
			return len(parts) == 3 && uuidName(parts[1]) && (parts[2] == "catalog.json" || parts[2] == "manifest.json")
		}
		return len(parts) == 2 && strings.HasSuffix(parts[1], ".json") && uuidName(strings.TrimSuffix(parts[1], ".json"))
	}
	if role == "journal" && parts[0] == "segments" {
		return len(parts) == 2 && uuidName(parts[1])
	}
	return role == "data" && len(parts) == 4 && parts[0] == "objects" && shardName(parts[1]) && uuidName(parts[2]) && parts[2][:2] == parts[1] && (parts[3] == "identity.json" || parts[3] == "payload")
}
func uuidName(s string) bool { _, err := fsapi.ParseUUID(s); return err == nil }
func shardName(s string) bool {
	if len(s) != 2 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && hex.EncodeToString(b) == s
}
