package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type idWire struct {
	Home       string `json:"home_uuid"`
	Allocation string `json:"allocation_uuid"`
}
type marker struct {
	Magic  string `json:"magic"`
	Major  uint16 `json:"major"`
	Minor  uint16 `json:"minor"`
	Role   string `json:"role"`
	Volume string `json:"volume_uuid"`
	Root   idWire `json:"root_object_id"`
}
type descriptorWire struct {
	Magic    string     `json:"magic"`
	Major    uint16     `json:"major"`
	Object   idWire     `json:"object_id"`
	Kind     fsapi.Kind `json:"type"`
	Creation string     `json:"creation_operation"`
}

// Format explicitly initializes two empty, private and distinct roots. A partial
// format is preserved and rejected by the next call; it is never guessed away.
// Root metadata and creation identity must come from protected initialization.
func Format(ctx context.Context, data, journal Filesystem, config Config, root Object, position fsapi.Position) (*Store, error) {
	s, err := newStore(data, journal, config)
	if err != nil {
		return nil, err
	}
	if err = s.validateRoots(ctx, true); err != nil {
		return nil, err
	}
	if root.Descriptor.Object != config.Root || root.Attr.Object != config.Root || root.Attr.Kind != fsapi.Directory || root.Parent == nil || root.Parent.Object != config.Root || root.Parent.EntryRevision != 0 || len(root.References) != 0 || root.Attr.Nlink != 2 || !validPosition(position, config.Volume) || position.Sequence != 0 {
		return nil, fmt.Errorf("%w: initial root", fsapi.Invalid)
	}
	s.objects[config.Root] = cloneObject(root)
	s.position = position
	if err = s.validateCatalog(); err != nil {
		return nil, err
	}
	for _, role := range []struct {
		fs   Filesystem
		name string
	}{{data, "data"}, {journal, "journal"}} {
		m := marker{"HOMELABFS-STORE", 1, 0, role.name, config.Volume.String(), toID(config.Root)}
		raw, e := jsonBytes(m)
		if e != nil {
			return nil, e
		}
		if e = writeExclusive(ctx, role.fs, "format.json", raw); e != nil {
			return nil, e
		}
		for _, dir := range []string{"checkpoints", "staging"} {
			if e = role.fs.Mkdir(ctx, dir, 0o700); e != nil {
				return nil, e
			}
		}
		if role.name == "data" {
			if e = role.fs.Mkdir(ctx, "objects", 0o700); e != nil {
				return nil, e
			}
		}
		if e = role.fs.SyncDir(ctx, "."); e != nil {
			return nil, e
		}
	}
	if err = s.ensureObject(ctx, root.Descriptor, false); err != nil {
		return nil, err
	}
	s.ready = true
	return s, nil
}

// Open admits an existing store only against an exact clean local baseline.
// Hash mismatch is unavailable, never permission to reset an existing payload.
// Reconstruction from dirty crash images belongs to the ordered replay engine.
func Open(ctx context.Context, data, journal Filesystem, config Config, image Image) (*Store, error) {
	s, err := newStore(data, journal, config)
	if err != nil {
		return nil, err
	}
	if err = s.validateRoots(ctx, false); err != nil {
		return nil, err
	}
	for _, r := range []struct {
		fs   Filesystem
		role string
	}{{data, "data"}, {journal, "journal"}} {
		raw, e := readPrivate(ctx, r.fs, "format.json", s.config.OwnerUID, s.config.Limits.FileBytes)
		if e != nil {
			return nil, e
		}
		var m marker
		if e = strictJSON(raw, &m); e != nil {
			return nil, e
		}
		expected := marker{"HOMELABFS-STORE", 1, 0, r.role, config.Volume.String(), toID(config.Root)}
		if m != expected {
			return nil, fmt.Errorf("%w: mismatching format marker", fsapi.Invalid)
		}
	}
	if err = s.loadImage(ctx, image); err != nil {
		return nil, err
	}
	s.ready = true
	return s, nil
}

func newStore(data, journal Filesystem, c Config) (*Store, error) {
	if data == nil || journal == nil || !c.Volume.Valid() || !c.Root.Valid() || c.Root.Home != c.Volume {
		return nil, fmt.Errorf("%w: store identity", fsapi.Invalid)
	}
	if c.Limits.Objects == 0 {
		c.Limits.Objects = 4096
	}
	if c.Limits.Entries == 0 {
		c.Limits.Entries = 16384
	}
	if c.Limits.Operations == 0 {
		c.Limits.Operations = 16384
	}
	if c.Limits.Pins == 0 {
		c.Limits.Pins = 4096
	}
	if c.Limits.FileBytes == 0 {
		c.Limits.FileBytes = 16 << 20
	}
	if c.Limits.Objects < 1 || c.Limits.Entries < 1 || c.Limits.Operations < 1 || c.Limits.Pins < 1 || c.Limits.FileBytes < 1 || c.Limits.FileBytes > 1<<30 {
		return nil, fmt.Errorf("%w: catalog limits", fsapi.Invalid)
	}
	if c.Random == nil {
		c.Random = rand.Reader
	}
	return &Store{data: data, journal: journal, config: c, objects: make(map[fsapi.ObjectID]Object), entries: make(map[entryKey]Entry), pins: make(map[fsapi.UUID]Pin), retired: make(map[fsapi.ObjectID]Descriptor), receipts: make(map[uint64]Receipt)}, nil
}

func (s *Store) validateRoots(ctx context.Context, empty bool) error {
	a, e := s.data.Stat(ctx, ".")
	if e != nil {
		return e
	}
	b, e := s.journal.Stat(ctx, ".")
	if e != nil {
		return e
	}
	if a.Identity == b.Identity {
		return fmt.Errorf("%w: aliased store roots", fsapi.Invalid)
	}
	for _, f := range []Filesystem{s.data, s.journal} {
		info, err := f.Stat(ctx, ".")
		if err != nil {
			return err
		}
		if !info.Mode.IsDir() || info.Mode != fs.ModeDir|0o700 || info.UID != s.config.OwnerUID {
			return fmt.Errorf("%w: private root admission", fsapi.Permission)
		}
		if empty {
			items, err := scanDir(ctx, f, ".", 1)
			if err != nil {
				return err
			}
			if len(items) != 0 {
				return fmt.Errorf("%w: format requires empty roots", fsapi.Exists)
			}
		}
	}
	return nil
}

func (s *Store) ensureObject(ctx context.Context, d Descriptor, reset bool) error {
	dir := objectPath(d.Object)
	for _, p := range []string{path.Dir(dir), dir} {
		if err := s.data.Mkdir(ctx, p, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := s.checkPrivate(ctx, p, true); err != nil {
			return err
		}
		if err := s.data.SyncDir(ctx, path.Dir(p)); err != nil {
			return err
		}
	}
	raw, err := jsonBytes(descriptorWire{"HOMELABFS-OBJECT", 1, toID(d.Object), d.Kind, d.Creation.String()})
	if err != nil {
		return err
	}
	if err = s.immutable(ctx, dir+"/identity.json", raw); err != nil {
		return err
	}
	if err = s.data.SyncDir(ctx, dir); err != nil {
		return err
	}
	if d.Kind != fsapi.Regular {
		return nil
	}
	flag := os.O_RDWR | os.O_CREATE | os.O_EXCL
	f, err := s.data.Open(ctx, dir+"/payload", flag, 0o600)
	if errors.Is(err, fs.ErrExist) {
		if !reset {
			return fmt.Errorf("%w: allocation already has payload", fsapi.Exists)
		}
		if err = s.checkPrivate(ctx, dir+"/payload", false); err != nil {
			return err
		}
		f, err = s.data.Open(ctx, dir+"/payload", os.O_RDWR, 0)
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if reset {
		if err = f.Truncate(ctx, 0); err != nil {
			return err
		}
	}
	if err = f.Sync(ctx); err != nil {
		return err
	}
	return s.data.SyncDir(ctx, dir)
}

func (s *Store) checkPrivate(ctx context.Context, p string, dir bool) error {
	info, err := s.data.Stat(ctx, p)
	if err != nil {
		return err
	}
	root, err := s.data.Stat(ctx, ".")
	if err != nil {
		return err
	}
	perm := fs.FileMode(0o600)
	if dir {
		perm = 0o700
	}
	if info.Mode.IsDir() != dir || (!dir && (!info.Mode.IsRegular() || info.Links != 1)) || info.Mode&^fs.ModeDir != perm || info.UID != s.config.OwnerUID || info.Identity.Device != root.Identity.Device {
		return fmt.Errorf("%w: private object admission %s", fsapi.Invalid, p)
	}
	return nil
}

func writeExclusive(ctx context.Context, f Filesystem, p string, raw []byte) error {
	file, err := f.Open(ctx, p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	n, err := file.WriteAt(ctx, raw, 0)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	if err = file.Sync(ctx); err != nil {
		return err
	}
	return f.SyncDir(ctx, path.Dir(p))
}

func readPrivate(ctx context.Context, f Filesystem, p string, owner uint32, max int64) ([]byte, error) {
	info, err := f.Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	root, err := f.Stat(ctx, ".")
	if err != nil {
		return nil, err
	}
	if !info.Mode.IsRegular() || info.Links != 1 || info.Mode != 0o600 || info.UID != owner || info.Identity.Device != root.Identity.Device || info.Size < 1 || info.Size > max {
		return nil, fmt.Errorf("%w: private file %s", fsapi.Invalid, p)
	}
	file, err := f.Open(ctx, p, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw := make([]byte, int(info.Size))
	n, err := file.ReadAt(ctx, raw, 0)
	if err != nil {
		return nil, err
	}
	if n != len(raw) {
		return nil, io.ErrUnexpectedEOF
	}
	return raw, nil
}

func strictJSON(raw []byte, out any) error {
	if !utf8.Valid(raw) || len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return fmt.Errorf("%w: JSON terminator", fsapi.Invalid)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := uniqueJSON(dec, reflect.TypeOf(out).Elem()); err != nil {
		return fmt.Errorf("%w: %v", fsapi.Invalid, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON", fsapi.Invalid)
	}
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", fsapi.Invalid, err)
	}
	return nil
}

func uniqueJSON(d *json.Decoder, typ reflect.Type) error {
	nullable := typ.Kind() == reflect.Pointer
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	if t == nil {
		if nullable {
			return nil
		}
		return fmt.Errorf("unexpected null")
	}
	delim, ok := t.(json.Delim)
	if !ok {
		if typ.Kind() == reflect.Struct || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			return fmt.Errorf("expected container")
		}
		return nil
	}
	switch delim {
	case '{':
		if typ.Kind() != reflect.Struct {
			return fmt.Errorf("unexpected object")
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := f.Tag.Get("json")
			if name == "" {
				name = f.Name
			}
			fields[name] = f.Type
		}
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			field, exists := fields[name]
			if !ok || seen[name] || !exists {
				return fmt.Errorf("duplicate/unknown JSON field %v", key)
			}
			seen[name] = true
			if e = uniqueJSON(d, field); e != nil {
				return e
			}
		}
		if len(seen) != len(fields) {
			return fmt.Errorf("missing JSON field")
		}
	case '[':
		if typ.Kind() != reflect.Slice && typ.Kind() != reflect.Array {
			return fmt.Errorf("unexpected array")
		}
		for d.More() {
			if e := uniqueJSON(d, typ.Elem()); e != nil {
				return e
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func jsonBytes(v any) ([]byte, error) { raw, err := json.Marshal(v); return append(raw, '\n'), err }
func toID(id fsapi.ObjectID) idWire   { return idWire{id.Home.String(), id.Allocation.String()} }
func objectPath(id fsapi.ObjectID) string {
	a := id.Allocation.String()
	return "objects/" + a[:2] + "/" + a
}
func validName(name string) bool {
	return name != "" && len(name) <= 255 && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}
func validPosition(p fsapi.Position, v fsapi.UUID) bool {
	return p.Volume == v && p.Generation.Valid() && p.Configuration.Valid() && p.Epoch != 0
}
func sameHistory(a, b fsapi.Position) bool { a.Sequence = 0; b.Sequence = 0; return a == b }
func lessID(a, b fsapi.ObjectID) int {
	if c := bytes.Compare(a.Home[:], b.Home[:]); c != 0 {
		return c
	}
	return bytes.Compare(a.Allocation[:], b.Allocation[:])
}
func cloneObject(o Object) Object {
	o.Text = slices.Clone(o.Text)
	o.References = slices.Clone(o.References)
	if o.Parent != nil {
		v := *o.Parent
		o.Parent = &v
	}
	return o
}
