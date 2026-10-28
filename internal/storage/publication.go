package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type reservation struct {
	Magic     string `json:"magic"`
	Major     uint16 `json:"major"`
	Operation string `json:"operation_uuid"`
	Object    idWire `json:"object_id"`
}
type manifest struct {
	Magic        string       `json:"magic"`
	Major        uint16       `json:"major"`
	Minor        uint16       `json:"minor"`
	Checkpoint   string       `json:"checkpoint_uuid"`
	Volume       string       `json:"volume_uuid"`
	Root         idWire       `json:"root_object_id"`
	Position     positionWire `json:"position"`
	CatalogBytes uint64       `json:"catalog_bytes"`
	CatalogHash  string       `json:"catalog_sha256"`
	Dependencies string       `json:"dependencies_base64"`
}
type pointer struct {
	Magic         string `json:"magic"`
	Major         uint16 `json:"major"`
	Checkpoint    string `json:"checkpoint_uuid"`
	ManifestBytes uint64 `json:"manifest_bytes"`
	ManifestHash  string `json:"manifest_sha256"`
}

// Allocate durably reserves a fresh random UUIDv4 for an operation. Reservations
// survive crashes and are checked alongside retained object identities and
// tombstones. Calling it again for the same operation returns the same ID.
func (s *Store) Allocate(ctx context.Context, operation fsapi.UUID) (fsapi.ObjectID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return fsapi.ObjectID{}, err
	}
	if !operation.Valid() {
		return fsapi.ObjectID{}, fsapi.Invalid
	}
	pending, err := s.reservations(ctx)
	if err != nil {
		return fsapi.ObjectID{}, err
	}
	if id, ok := pending[operation]; ok {
		dir := "staging/" + operation.String()
		if err = s.syncFiles(ctx, dir, []string{"allocation.json"}); err != nil {
			return fsapi.ObjectID{}, err
		}
		if err = s.data.SyncDir(ctx, "staging"); err != nil {
			return fsapi.ObjectID{}, err
		}
		return id, nil
	}
	if len(pending)+len(s.objects)+len(s.retired) >= s.config.Limits.Objects {
		return fsapi.ObjectID{}, fsapi.NoSpace
	}
	for attempt := 0; attempt < 32; attempt++ {
		var allocation fsapi.UUID
		if _, err = io.ReadFull(s.config.Random, allocation[:]); err != nil {
			return fsapi.ObjectID{}, err
		}
		allocation[6] = allocation[6]&0x0f | 0x40
		allocation[8] = allocation[8]&0x3f | 0x80
		id := fsapi.ObjectID{Home: s.config.Volume, Allocation: allocation}
		used := false
		for _, v := range pending {
			if v == id {
				used = true
			}
		}
		if _, ok := s.objects[id]; ok {
			used = true
		}
		if _, ok := s.retired[id]; ok {
			used = true
		}
		_, err = s.data.Stat(ctx, objectPath(id))
		if err == nil {
			used = true
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fsapi.ObjectID{}, err
		}
		if used {
			continue
		}
		dir := "staging/" + operation.String()
		if err = s.mkdirPrivate(ctx, dir); err != nil {
			return fsapi.ObjectID{}, err
		}
		raw, e := jsonBytes(reservation{"HOMELABFS-ALLOCATION", 1, operation.String(), toID(id)})
		if e != nil {
			return fsapi.ObjectID{}, e
		}
		if e = writeExclusive(ctx, s.data, dir+"/allocation.json", raw); e != nil {
			return fsapi.ObjectID{}, e
		}
		return id, nil
	}
	return fsapi.ObjectID{}, fmt.Errorf("%w: allocation collision budget", fsapi.NoSpace)
}

// StageCheckpoint writes immutable catalog/manifest files into private staging.
// Dependencies are exact caller-supplied protocol evidence, retained as bytes;
// they are not interpreted as authorization. Image must match the current cut.
func (s *Store) StageCheckpoint(ctx context.Context, id fsapi.UUID, image Image, dependencies []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return err
	}
	if !id.Valid() || int64(len(dependencies)) > s.config.Limits.FileBytes {
		return fsapi.Invalid
	}
	if s.checkpoint.Valid() && s.checkpoint != id {
		return fsapi.Busy
	}
	if err := s.matchesImage(ctx, image); err != nil {
		return err
	}
	raw, err := EncodeImage(image, s.config.Limits)
	if err != nil {
		return err
	}
	if int64(len(raw)) > s.config.Limits.FileBytes {
		return fsapi.NoSpace
	}
	p := image.Position
	digest := sha256.Sum256(raw)
	m := manifest{"HOMELABFS-MANIFEST", 1, 0, id.String(), s.config.Volume.String(), toID(s.config.Root), positionWire{p.Volume.String(), p.Generation.String(), p.Configuration.String(), p.Epoch, p.Sequence}, uint64(len(raw)), hex.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString(dependencies)}
	manifestRaw, err := jsonBytes(m)
	if err != nil {
		return err
	}
	if int64(len(manifestRaw)) > s.config.Limits.FileBytes {
		return fsapi.NoSpace
	}
	dest := "checkpoints/" + id.String()
	if _, e := s.data.Stat(ctx, dest); e == nil {
		for _, v := range []struct {
			name string
			raw  []byte
		}{{"catalog.json", raw}, {"manifest.json", manifestRaw}} {
			old, e := readPrivate(ctx, s.data, dest+"/"+v.name, s.config.OwnerUID, s.config.Limits.FileBytes)
			if e != nil {
				return e
			}
			if !bytes.Equal(old, v.raw) {
				return fmt.Errorf("%w: checkpoint identity reused", fsapi.Stale)
			}
		}
		s.checkpoint = id
		return s.syncFiles(ctx, dest, []string{"catalog.json", "manifest.json"})
	} else if !errors.Is(e, fs.ErrNotExist) {
		return e
	}
	s.checkpoint = id
	dir := "staging/" + id.String()
	if err = s.mkdirPrivate(ctx, dir); err != nil {
		return err
	}
	for _, v := range []struct {
		name string
		raw  []byte
	}{{"catalog.json", raw}, {"manifest.json", manifestRaw}} {
		if err = s.immutable(ctx, dir+"/"+v.name, v.raw); err != nil {
			return err
		}
	}
	return s.data.SyncDir(ctx, dir)
}

// PublishCheckpoint renames a staged immutable directory and syncs both parents.
// It does not write journal acceptance or select CURRENT. Repeated publication
// validates the destination rather than overwriting an unrelated checkpoint.
func (s *Store) PublishCheckpoint(ctx context.Context, id fsapi.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return err
	}
	if !id.Valid() {
		return fsapi.Invalid
	}
	dest := "checkpoints/" + id.String()
	if _, err := s.data.Stat(ctx, dest); err == nil {
		if _, err = s.verifyCheckpoint(ctx, id, dest); err != nil {
			return err
		}
		if err = s.syncFiles(ctx, dest, []string{"catalog.json", "manifest.json"}); err != nil {
			return err
		}
		if err = s.data.SyncDir(ctx, "staging"); err != nil {
			return err
		}
		return s.data.SyncDir(ctx, "checkpoints")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	src := "staging/" + id.String()
	if _, err := s.verifyCheckpoint(ctx, id, src); err != nil {
		return err
	}
	items, err := scanDir(ctx, s.data, src, 3)
	if err != nil {
		return err
	}
	if len(items) != 2 {
		return fmt.Errorf("%w: mixed checkpoint staging", fsapi.Invalid)
	}
	if err = s.syncFiles(ctx, src, []string{"catalog.json", "manifest.json"}); err != nil {
		return err
	}
	if err = s.data.Rename(ctx, src, dest); err != nil {
		return err
	}
	if err = s.data.SyncDir(ctx, "staging"); err != nil {
		return err
	}
	return s.data.SyncDir(ctx, "checkpoints")
}

// PublishCurrent atomically selects a durable immutable local manifest. The
// trusted caller must already have protected matching journal acceptance for
// the entire closure. This helper supplies only local write/sync/rename barriers.
// It never authorizes serving, peer publication or retention by itself.
func (s *Store) PublishCurrent(ctx context.Context, id, publication fsapi.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return err
	}
	if !id.Valid() || !publication.Valid() {
		return fsapi.Invalid
	}
	raw, err := s.verifyCheckpoint(ctx, id, "checkpoints/"+id.String())
	if err != nil {
		return err
	}
	cat, err := readPrivate(ctx, s.data, "checkpoints/"+id.String()+"/catalog.json", s.config.OwnerUID, s.config.Limits.FileBytes)
	if err != nil {
		return err
	}
	image, err := DecodeImage(cat, s.config.Limits)
	if err != nil {
		return err
	}
	if err = s.matchesImage(ctx, image); err != nil {
		return err
	}
	h := sha256.Sum256(raw)
	ptr, err := jsonBytes(pointer{"HOMELABFS-CURRENT", 1, id.String(), uint64(len(raw)), hex.EncodeToString(h[:])})
	if err != nil {
		return err
	}
	temp := "CURRENT.tmp-" + publication.String()
	if err = s.immutable(ctx, temp, ptr); err != nil {
		return err
	}
	if err = s.data.Rename(ctx, temp, "CURRENT"); err != nil {
		return err
	}
	if err = s.data.SyncDir(ctx, "."); err != nil {
		return err
	}
	s.checkpoint = fsapi.UUID{}
	return nil
}

// ReadCurrent validates the pointer, manifest hash/length and catalog hash/length
// without treating them as distributed checkpoint acceptance or authority.
func (s *Store) ReadCurrent(ctx context.Context) (fsapi.UUID, Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := readPrivate(ctx, s.data, "CURRENT", s.config.OwnerUID, s.config.Limits.FileBytes)
	if err != nil {
		return fsapi.UUID{}, Image{}, err
	}
	var p pointer
	if err = strictJSON(raw, &p); err != nil {
		return fsapi.UUID{}, Image{}, err
	}
	id, err := fsapi.ParseUUID(p.Checkpoint)
	if err != nil {
		return fsapi.UUID{}, Image{}, err
	}
	if p.Magic != "HOMELABFS-CURRENT" || p.Major != 1 {
		return fsapi.UUID{}, Image{}, fsapi.Invalid
	}
	dir := "checkpoints/" + id.String()
	m, err := s.verifyCheckpoint(ctx, id, dir)
	if err != nil {
		return fsapi.UUID{}, Image{}, err
	}
	h := sha256.Sum256(m)
	if uint64(len(m)) != p.ManifestBytes || hex.EncodeToString(h[:]) != p.ManifestHash {
		return fsapi.UUID{}, Image{}, fsapi.IO
	}
	catalog, err := readPrivate(ctx, s.data, dir+"/catalog.json", s.config.OwnerUID, s.config.Limits.FileBytes)
	if err != nil {
		return fsapi.UUID{}, Image{}, err
	}
	image, err := DecodeImage(catalog, s.config.Limits)
	return id, image, err
}

func (s *Store) reservations(ctx context.Context) (map[fsapi.UUID]fsapi.ObjectID, error) {
	dirs, err := scanDir(ctx, s.data, "staging", s.config.Limits.Operations)
	if err != nil {
		return nil, err
	}
	out := map[fsapi.UUID]fsapi.ObjectID{}
	seen := map[fsapi.ObjectID]bool{}
	for _, d := range dirs {
		if !d.Info.Mode.IsDir() || !uuidName(d.Name) {
			return nil, fsapi.Invalid
		}
		p := "staging/" + d.Name + "/allocation.json"
		raw, e := readPrivate(ctx, s.data, p, s.config.OwnerUID, s.config.Limits.FileBytes)
		if errors.Is(e, fs.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var r reservation
		if e = strictJSON(raw, &r); e != nil {
			return nil, e
		}
		op, e := fsapi.ParseUUID(r.Operation)
		if e != nil {
			return nil, e
		}
		id, e := fromID(r.Object)
		if e != nil {
			return nil, e
		}
		if r.Magic != "HOMELABFS-ALLOCATION" || r.Major != 1 || r.Operation != d.Name || id.Home != s.config.Volume || seen[id] {
			return nil, fsapi.Invalid
		}
		seen[id] = true
		out[op] = id
	}
	return out, nil
}
func (s *Store) mkdirPrivate(ctx context.Context, p string) error {
	if err := s.data.Mkdir(ctx, p, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := s.checkPrivate(ctx, p, true); err != nil {
		return err
	}
	return s.data.SyncDir(ctx, path.Dir(p))
}
func (s *Store) immutable(ctx context.Context, p string, raw []byte) error {
	err := writeExclusive(ctx, s.data, p, raw)
	if !errors.Is(err, fs.ErrExist) {
		return err
	}
	old, e := readPrivate(ctx, s.data, p, s.config.OwnerUID, s.config.Limits.FileBytes)
	if e != nil {
		return e
	}
	if !bytes.Equal(old, raw) {
		return fmt.Errorf("%w: immutable artifact conflict", fsapi.Stale)
	}
	f, e := s.data.Open(ctx, p, os.O_RDONLY, 0)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	if e = f.Sync(ctx); e != nil {
		return e
	}
	return s.data.SyncDir(ctx, path.Dir(p))
}
func (s *Store) verifyCheckpoint(ctx context.Context, id fsapi.UUID, dir string) ([]byte, error) {
	if err := s.checkPrivate(ctx, dir, true); err != nil {
		return nil, err
	}
	raw, err := readPrivate(ctx, s.data, dir+"/manifest.json", s.config.OwnerUID, s.config.Limits.FileBytes)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err = strictJSON(raw, &m); err != nil {
		return nil, err
	}
	if m.Magic != "HOMELABFS-MANIFEST" || m.Major != 1 || m.Minor != 0 || m.Checkpoint != id.String() || m.Volume != s.config.Volume.String() || m.Root != toID(s.config.Root) {
		return nil, fsapi.Invalid
	}
	if _, err = decodeBase64(m.Dependencies); err != nil {
		return nil, err
	}
	cat, err := readPrivate(ctx, s.data, dir+"/catalog.json", s.config.OwnerUID, s.config.Limits.FileBytes)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(cat)
	if uint64(len(cat)) != m.CatalogBytes || hex.EncodeToString(h[:]) != m.CatalogHash {
		return nil, fsapi.IO
	}
	image, err := DecodeImage(cat, s.config.Limits)
	if err != nil {
		return nil, err
	}
	p, err := fromPosition(m.Position)
	if err != nil || p != image.Position || image.Root != s.config.Root || image.Volume != s.config.Volume {
		return nil, fsapi.Invalid
	}
	return raw, nil
}
func (s *Store) matchesImage(ctx context.Context, i Image) error {
	if err := validateImage(i, s.config.Limits); err != nil {
		return err
	}
	if i.Position != s.position || i.Volume != s.config.Volume || i.Root != s.config.Root || len(i.Objects) != len(s.objects) || len(i.Entries) != len(s.entries) || len(i.Pins) != len(s.pins) || len(i.Retired) != len(s.retired) || len(i.Receipts) != len(s.receipts) {
		return fsapi.Stale
	}
	for _, o := range i.Objects {
		if !reflect.DeepEqual(o, s.objects[o.Attr.Object]) {
			return fsapi.Stale
		}
		if err := s.syncObject(ctx, o); err != nil {
			return err
		}
	}
	for _, e := range i.Entries {
		if !reflect.DeepEqual(e, s.entries[entryKey{e.Parent, e.Name}]) {
			return fsapi.Stale
		}
	}
	for _, p := range i.Pins {
		if p != s.pins[p.ID] {
			return fsapi.Stale
		}
	}
	for _, d := range i.Retired {
		if d != s.retired[d.Object] {
			return fsapi.Stale
		}
	}
	for _, r := range i.Receipts {
		if r != s.receipts[r.Sequence] {
			return fsapi.Stale
		}
	}
	for _, p := range i.Payloads {
		actual, err := s.hashPayload(ctx, s.objects[p.Object])
		if err != nil {
			return err
		}
		if actual != p {
			return fsapi.Stale
		}
	}
	return nil
}

func (s *Store) checkReservation(ctx context.Context, d Descriptor) error {
	pending, err := s.reservations(ctx)
	if err != nil {
		return err
	}
	for op, id := range pending {
		if id == d.Object && op != d.Creation {
			return fmt.Errorf("%w: reserved allocation belongs to another operation", fsapi.Stale)
		}
	}
	return nil
}

func (s *Store) syncFiles(ctx context.Context, dir string, names []string) error {
	for _, name := range names {
		p := dir + "/" + name
		if err := s.checkPrivate(ctx, p, false); err != nil {
			return err
		}
		f, err := s.data.Open(ctx, p, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		err = f.Sync(ctx)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return s.data.SyncDir(ctx, dir)
}
