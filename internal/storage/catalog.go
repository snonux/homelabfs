package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"slices"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Apply installs one trusted committed primitive. Duplicate slots must contain
// identical semantic bytes and return without touching payloads or newer names.
// A gap, stale revision or conflicting duplicate cannot advance the catalog.
func (s *Store) Apply(ctx context.Context, op Operation) error {
	op = cloneOperation(op)
	raw, err := operationBytes(op)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	s.mu.Lock()
	if r, ok := s.receipts[op.Position.Sequence]; ok {
		s.mu.Unlock()
		if r.ID == op.ID && r.SHA256 == digest {
			return nil
		}
		return fmt.Errorf("%w: conflicting duplicate", fsapi.Stale)
	}
	s.mu.Unlock()
	homes, err := s.operationHomes(op)
	if err != nil {
		return err
	}
	if len(homes) > 1 {
		if s.config.Closure == nil {
			return fmt.Errorf("%w: foreign-home operation needs coordinated closure", fsapi.Unavailable)
		}
		release, e := s.config.Closure.Enter(ctx, op, homes)
		if e != nil {
			return e
		}
		if release == nil {
			return fmt.Errorf("%w: missing closure gate", fsapi.Unavailable)
		}
		defer release()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	if !s.ready {
		return fsapi.Unavailable
	}
	if s.checkpoint.Valid() {
		return fsapi.Busy
	}
	if !op.ID.Valid() || !op.Object.Valid() || !validPosition(op.Position, s.config.Volume) || !sameHistory(op.Position, s.position) || op.Position.Sequence == 0 || op.Kind < Create || op.Kind > Reclaim {
		return fmt.Errorf("%w: operation identity/history", fsapi.Invalid)
	}
	if r, ok := s.receipts[op.Position.Sequence]; ok {
		if r.ID == op.ID && r.SHA256 == digest {
			return nil
		}
		return fmt.Errorf("%w: conflicting duplicate", fsapi.Stale)
	}
	for _, r := range s.receipts {
		if r.ID == op.ID {
			return fmt.Errorf("%w: operation identity reused", fsapi.Stale)
		}
	}
	if s.position.Sequence == math.MaxUint64 || op.Position.Sequence != s.position.Sequence+1 {
		return fmt.Errorf("%w: noncontiguous operation", fsapi.Stale)
	}
	next := s.copyCatalog()
	if err = next.installCatalog(op); err != nil {
		return err
	}
	if err = next.validatePrimitive(s, op); err != nil {
		return err
	}
	if err = next.validateCatalog(); err != nil {
		return err
	}
	if len(s.receipts) >= s.config.Limits.Operations {
		return fsapi.NoSpace
	}
	for _, change := range op.Objects {
		if change.Before == 0 {
			if err = s.checkReservation(ctx, change.After.Descriptor); err != nil {
				return err
			}
		}
	}
	// Close serving before the first fallible physical mutation. A failed apply
	// leaves original catalog and all evidence intact, requiring clean recovery.
	s.ready = false
	for _, change := range op.Objects {
		if change.Before == 0 {
			if err = s.ensureObject(ctx, change.After.Descriptor, true); err != nil {
				return err
			}
		}
	}
	if op.Kind == Write || op.Kind == Truncate {
		if err = s.applyPayload(ctx, op); err != nil {
			return err
		}
	}
	if op.Kind == Reclaim {
		if err = s.removePayload(ctx, op.Object); err != nil {
			return err
		}
	}
	s.objects = next.objects
	s.entries = next.entries
	s.pins = next.pins
	s.retired = next.retired
	s.receipts[op.Position.Sequence] = Receipt{op.Position.Sequence, op.ID, digest}
	s.position = op.Position
	s.ready = true
	return nil
}

// Stat returns committed metadata for an exact ID, including unlinked objects.
func (s *Store) Stat(ctx context.Context, id fsapi.ObjectID) (Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return Object{}, err
	}
	o, ok := s.objects[id]
	if !ok {
		return Object{}, fsapi.NotFound
	}
	return cloneObject(o), nil
}

// Lookup resolves one raw logical name without consulting host directory names.
// Foreign target IDs are returned for the service to route to their home.
func (s *Store) Lookup(ctx context.Context, parent fsapi.ObjectID, name string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return Entry{}, err
	}
	if !validName(name) {
		return Entry{}, fsapi.Invalid
	}
	if err := s.directory(parent); err != nil {
		return Entry{}, err
	}
	e, ok := s.entries[entryKey{parent, name}]
	if !ok || e.Target == nil {
		return Entry{}, fsapi.NotFound
	}
	return cloneEntry(e), nil
}

// ReadDir returns a bounded, sorted raw-name page after the exclusive name cursor.
// Tombstones and synthesized dot entries are not returned. The service binds a
// traversal to its directory revision and visibility generation.
func (s *Store) ReadDir(ctx context.Context, parent fsapi.ObjectID, after string, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1024 {
		return nil, fsapi.Invalid
	}
	if err := s.directory(parent); err != nil {
		return nil, err
	}
	out := make([]Entry, 0)
	for k, e := range s.entries {
		if k.parent == parent && k.name > after && e.Target != nil {
			out = append(out, cloneEntry(e))
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return bytes.Compare([]byte(a.Name), []byte(b.Name)) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Read reads a committed logical range by immutable home ID. Reads never change
// atime. Unlinked payloads stay readable until explicitly protected reclamation.
func (s *Store) Read(ctx context.Context, id fsapi.ObjectID, p []byte, offset int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readable(ctx); err != nil {
		return 0, err
	}
	if err := fsapi.ValidateRange(offset, int64(len(p))); err != nil {
		return 0, err
	}
	o, ok := s.objects[id]
	if !ok {
		return 0, fsapi.NotFound
	}
	if o.Attr.Kind != fsapi.Regular {
		return 0, fsapi.Invalid
	}
	if len(p) == 0 {
		return 0, nil
	}
	if offset >= o.Attr.Size {
		return 0, io.EOF
	}
	amount := int64(len(p))
	short := amount > o.Attr.Size-offset
	if short {
		amount = o.Attr.Size - offset
	}
	if err := s.checkPrivate(ctx, objectPath(id)+"/payload", false); err != nil {
		return 0, err
	}
	f, err := s.data.Open(ctx, objectPath(id)+"/payload", os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	n, err := f.ReadAt(ctx, p[:amount], offset)
	if err == nil && short {
		err = io.EOF
	}
	return n, err
}

func (s *Store) copyCatalog() *Store {
	n := &Store{config: s.config, objects: make(map[fsapi.ObjectID]Object, len(s.objects)), entries: make(map[entryKey]Entry, len(s.entries)), pins: make(map[fsapi.UUID]Pin, len(s.pins)), retired: make(map[fsapi.ObjectID]Descriptor, len(s.retired))}
	for k, v := range s.objects {
		n.objects[k] = v
	}
	for k, v := range s.entries {
		n.entries[k] = v
	}
	for k, v := range s.pins {
		n.pins[k] = v
	}
	for k, v := range s.retired {
		n.retired[k] = v
	}
	return n
}

func (s *Store) installCatalog(op Operation) error {
	seenObjects := map[fsapi.ObjectID]bool{}
	for _, c := range op.Objects {
		id := c.After.Attr.Object
		old, exists := s.objects[id]
		if seenObjects[id] || id.Home != s.config.Volume || !id.Valid() {
			return fmt.Errorf("%w: duplicate/foreign object edit", fsapi.Invalid)
		}
		seenObjects[id] = true
		if !exists {
			if c.Before != 0 || c.After.Attr.Revision != 1 || c.After.Descriptor.Creation != op.ID {
				return fsapi.Stale
			}
			if _, used := s.retired[id]; used {
				return fsapi.Stale
			}
		} else {
			if c.Before != old.Attr.Revision || c.Before == math.MaxUint64 || c.After.Attr.Revision != c.Before+1 || c.After.Descriptor != old.Descriptor {
				return fsapi.Stale
			}
			if c.After.LedgerRevision != old.LedgerRevision {
				if old.LedgerRevision == math.MaxUint64 || c.After.LedgerRevision != old.LedgerRevision+1 {
					return fsapi.Stale
				}
			} else if !reflect.DeepEqual(c.After.References, old.References) || old.Attr.Kind != fsapi.Directory && c.After.Attr.Nlink != old.Attr.Nlink {
				return fsapi.Stale
			}
			if !reflect.DeepEqual(c.After.Parent, old.Parent) && (c.After.Parent == nil || old.Parent == nil || old.Parent.Revision == math.MaxUint64 || c.After.Parent.Revision != old.Parent.Revision+1) {
				return fsapi.Stale
			}
		}
		s.objects[id] = cloneObject(c.After)
	}
	seenEntries := map[entryKey]bool{}
	for _, c := range op.Entries {
		k := entryKey{c.After.Parent, c.After.Name}
		if seenEntries[k] || c.Before.Parent != k.parent || c.Before.Name != k.name || k.parent.Home != s.config.Volume || !validName(k.name) {
			return fsapi.Invalid
		}
		seenEntries[k] = true
		old, ok := s.entries[k]
		if !ok {
			old = Entry{Parent: k.parent, Name: k.name}
		}
		if !reflect.DeepEqual(old, c.Before) || old.Revision == math.MaxUint64 || c.After.Revision != old.Revision+1 {
			return fsapi.Stale
		}
		parent, exists := s.objects[k.parent]
		if !exists || parent.Attr.Kind != fsapi.Directory {
			return fsapi.NotDirectory
		}
		if _, changed := seenObjects[k.parent]; !changed {
			return fmt.Errorf("%w: entry edit lacks parent revision", fsapi.Invalid)
		}
		s.entries[k] = cloneEntry(c.After)
	}
	seenPins := map[fsapi.UUID]bool{}
	for _, c := range op.Pins {
		p := c.After
		old, exists := s.pins[p.ID]
		if seenPins[p.ID] || !p.ID.Valid() || !p.Object.Valid() || p.Object.Home != s.config.Volume || !p.Generation.Valid() || !p.Incarnation.Valid() {
			return fsapi.Invalid
		}
		seenPins[p.ID] = true
		if c.Before == nil {
			if exists || !p.Active {
				return fsapi.Stale
			}
		} else if !exists || old != *c.Before || !old.Active || p.Active || p.ID != old.ID || p.Object != old.Object || p.Generation != old.Generation || p.Incarnation != old.Incarnation {
			return fsapi.Stale
		}
		if _, exists := s.objects[p.Object]; !exists {
			return fsapi.NotFound
		}
		s.pins[p.ID] = p
	}
	if op.Kind == Reclaim {
		o, exists := s.objects[op.Object]
		if !exists || op.Object == s.config.Root {
			return fsapi.NotFound
		}
		if o.Attr.Revision != op.ExpectedRevision || o.LedgerRevision != op.ExpectedLedgerRevision {
			return fsapi.Stale
		}
		if o.Attr.Nlink != 0 || len(o.References) != 0 {
			return fsapi.Busy
		}
		for _, p := range s.pins {
			if p.Active && p.Object == op.Object {
				return fsapi.Busy
			}
		}
		for _, e := range s.entries {
			if e.Target != nil && (e.Parent == op.Object || *e.Target == op.Object) {
				return fsapi.Busy
			}
		}
		s.retired[op.Object] = o.Descriptor
		delete(s.objects, op.Object)
	}
	return nil
}

func (s *Store) validatePrimitive(before *Store, op Operation) error {
	if op.Kind != Reclaim && (op.ExpectedRevision != 0 || op.ExpectedLedgerRevision != 0) {
		return fsapi.Invalid
	}
	// Reject payload effects disguised as another operation.
	if op.Kind != Write && op.Kind != Truncate && (len(op.Data) != 0 || op.Offset != 0 || op.Size != 0) {
		return fsapi.Invalid
	}
	if op.Kind != HandlePin && op.Kind != HandleRelease && len(op.Pins) != 0 {
		return fsapi.Invalid
	}
	creations := 0
	for _, c := range op.Objects {
		id := c.After.Attr.Object
		old, exists := before.objects[id]
		if !exists {
			creations++
			if op.Kind != Create && op.Kind != Mkdir && op.Kind != Symlink {
				return fsapi.Invalid
			}
		}
		if exists && c.After.Attr.Size != old.Attr.Size && (op.Kind != Write && op.Kind != Truncate || id != op.Object) {
			return fsapi.Invalid
		}
		if exists && !bytes.Equal(c.After.Text, old.Text) {
			return fsapi.Invalid
		}
	}
	switch op.Kind {
	case Create, Mkdir, Symlink:
		kind := fsapi.Regular
		if op.Kind == Mkdir {
			kind = fsapi.Directory
		}
		if op.Kind == Symlink {
			kind = fsapi.Symlink
		}
		o, ok := s.objects[op.Object]
		if !ok || creations != 1 || o.Attr.Kind != kind || o.Attr.Revision != 1 || o.Attr.Nlink == 0 || kind == fsapi.Regular && o.Attr.Size != 0 || len(op.Entries) != 1 || op.Entries[0].Before.Target != nil || op.Entries[0].After.Target == nil || *op.Entries[0].After.Target != op.Object {
			return fsapi.Invalid
		}
	case Write, Truncate:
		old, ok := before.objects[op.Object]
		o, exists := s.objects[op.Object]
		if !ok || !exists || old.Attr.Kind != fsapi.Regular || len(op.Objects) != 1 || len(op.Entries) != 0 {
			return fsapi.Invalid
		}
		if op.Kind == Write {
			if len(op.Data) == 0 || int64(len(op.Data)) > s.config.Limits.FileBytes || fsapi.ValidateRange(op.Offset, int64(len(op.Data))) != nil || op.Size != 0 {
				return fsapi.Invalid
			}
			expected := max(old.Attr.Size, op.Offset+int64(len(op.Data)))
			if o.Attr.Size != expected {
				return fsapi.Invalid
			}
		} else if op.Offset != 0 || len(op.Data) != 0 || op.Size < 0 || o.Attr.Size != op.Size {
			return fsapi.Invalid
		}
	case Setattr:
		if len(op.Objects) != 1 || len(op.Entries) != 0 {
			return fsapi.Invalid
		}
		c := op.Objects[0]
		old, ok := before.objects[c.After.Attr.Object]
		if !ok || c.After.Attr.Object != op.Object || old.Attr.Nlink != c.After.Attr.Nlink || !reflect.DeepEqual(old.References, c.After.References) || !reflect.DeepEqual(old.Parent, c.After.Parent) {
			return fsapi.Invalid
		}
	case Link:
		o, ok := s.objects[op.Object]
		if op.Object.Home == s.config.Volume && (!ok || o.Attr.Kind != fsapi.Regular) {
			return fsapi.Invalid
		}
		if len(op.Entries) == 0 && len(op.Participants) > 1 && op.Object.Home == s.config.Volume && len(op.Objects) > 0 {
			break
		}
		if len(op.Entries) != 1 || op.Entries[0].Before.Target != nil || op.Entries[0].After.Target == nil || *op.Entries[0].After.Target != op.Object {
			return fsapi.Invalid
		}
	case Unlink, Rmdir:
		if o, ok := before.objects[op.Object]; ok {
			if (op.Kind == Rmdir) != (o.Attr.Kind == fsapi.Directory) {
				return fsapi.Invalid
			}
			if op.Kind == Rmdir {
				for _, e := range before.entries {
					if e.Parent == op.Object && e.Target != nil {
						return fsapi.NotEmpty
					}
				}
			}
		}
		if len(op.Entries) == 0 && len(op.Participants) > 1 && op.Object.Home == s.config.Volume && len(op.Objects) > 0 {
			break
		}
		if len(op.Entries) != 1 || op.Entries[0].Before.Target == nil || *op.Entries[0].Before.Target != op.Object || op.Entries[0].After.Target != nil {
			return fsapi.Invalid
		}
	case Rename:
		if len(op.Entries) == 0 {
			if len(op.Objects) != 0 && len(op.Participants) < 2 {
				return fsapi.Invalid
			}
			return nil
		}
		if len(op.Entries) > 2 || len(op.Entries) != 2 && len(op.Participants) < 2 {
			return fsapi.Invalid
		}
		var removed, added int
		for _, e := range op.Entries {
			if e.Before.Target != nil && e.After.Target != nil && *e.Before.Target == op.Object && *e.After.Target == op.Object {
				return fmt.Errorf("%w: same-object rename must be a no-op", fsapi.Invalid)
			}
			if e.Before.Target != nil && *e.Before.Target == op.Object && e.After.Target == nil {
				removed++
			}
			if e.After.Target != nil && *e.After.Target == op.Object {
				added++
				if e.Before.Target != nil {
					if old, ok := before.objects[*e.Before.Target]; ok {
						if source, found := before.objects[op.Object]; found && (old.Attr.Kind == fsapi.Directory) != (source.Attr.Kind == fsapi.Directory) {
							return fsapi.Invalid
						}
					}
					if old, ok := before.objects[*e.Before.Target]; ok && old.Attr.Kind == fsapi.Directory {
						for _, child := range before.entries {
							if child.Parent == old.Attr.Object && child.Target != nil {
								return fsapi.NotEmpty
							}
						}
					}
				}
			}
		}
		if len(op.Participants) > 1 && len(op.Entries) == 1 && removed+added == 1 {
			break
		}
		if removed != 1 || added != 1 {
			return fsapi.Invalid
		}
	case HandlePin, HandleRelease:
		if len(op.Objects) != 0 || len(op.Entries) != 0 || len(op.Pins) != 1 || op.Pins[0].After.Object != op.Object || op.Pins[0].After.Active != (op.Kind == HandlePin) {
			return fsapi.Invalid
		}
	case Reclaim:
		if len(op.Objects) != 0 || len(op.Entries) != 0 || len(op.Pins) != 0 {
			return fsapi.Invalid
		}
	}
	return nil
}

func (s *Store) validateCatalog() error {
	l := s.config.Limits
	if len(s.objects)+len(s.retired) > l.Objects || len(s.entries) > l.Entries || len(s.pins) > l.Pins {
		return fsapi.NoSpace
	}
	root, ok := s.objects[s.config.Root]
	if !ok || root.Attr.Kind != fsapi.Directory || root.Parent == nil || root.Parent.Object != s.config.Root || root.Parent.EntryRevision != 0 || len(root.References) != 0 || root.Attr.Nlink < 2 {
		return fsapi.Invalid
	}
	for id, o := range s.objects {
		if err := validateObject(o, s.config.Volume); err != nil {
			return err
		}
		if id != o.Attr.Object {
			return fsapi.Invalid
		}
		if o.Attr.Kind != fsapi.Directory {
			if o.Attr.Nlink != uint64(len(o.References)) {
				return fmt.Errorf("%w: home link ledger", fsapi.Invalid)
			}
		} else if id != s.config.Root && len(o.References) > 1 {
			return fsapi.Invalid
		}
		for _, r := range o.References {
			if r.Parent.Home == s.config.Volume {
				e, ok := s.entries[entryKey{r.Parent, r.Name}]
				if !ok || e.Target == nil || *e.Target != id || e.Revision != r.Revision {
					return fmt.Errorf("%w: ledger lacks exact entry", fsapi.Invalid)
				}
			}
		}
		if o.Attr.Kind == fsapi.Directory {
			if id != s.config.Root {
				if len(o.References) == 0 {
					if o.Attr.Nlink != 0 {
						return fsapi.Invalid
					}
				} else {
					r := o.References[0]
					if o.Parent.Object != r.Parent || o.Parent.EntryRevision != r.Revision {
						return fsapi.Invalid
					}
				}
			}
			expected := uint64(2)
			for _, e := range s.entries {
				if e.Parent == id && e.Target != nil {
					if o.Attr.Nlink == 0 {
						return fmt.Errorf("%w: removed directory has live children", fsapi.Invalid)
					}
					if child, ok := s.objects[*e.Target]; ok && child.Attr.Kind == fsapi.Directory {
						expected++
					}
				}
			}
			if o.Attr.Nlink != 0 && o.Attr.Nlink != expected && !s.hasForeign(id) {
				return fmt.Errorf("%w: directory link accounting", fsapi.Invalid)
			}
			visited := map[fsapi.ObjectID]bool{}
			cur := id
			for cur != s.config.Root {
				if visited[cur] {
					return fmt.Errorf("%w: directory ancestry cycle", fsapi.Invalid)
				}
				visited[cur] = true
				v, exists := s.objects[cur]
				if !exists || v.Parent == nil {
					break
				}
				cur = v.Parent.Object
				if cur.Home != s.config.Volume {
					break
				}
			}
		}
	}
	for _, e := range s.entries {
		if !validName(e.Name) || e.Revision == 0 {
			return fsapi.Invalid
		}
		if err := s.directory(e.Parent); err != nil {
			// Reclaimed directories retain their exact name tombstones. Their
			// permanent descriptor proves parent identity/type without keeping
			// an otherwise dead directory live. Live entries still need a live
			// directory and cannot be legitimized by allocation evidence.
			d, retired := s.retired[e.Parent]
			if e.Target != nil || !retired || d.Kind != fsapi.Directory {
				return err
			}
		}
		if e.Target == nil {
			continue
		}
		if !e.Target.Valid() {
			return fsapi.Invalid
		}
		if e.Target.Home == s.config.Volume {
			o, ok := s.objects[*e.Target]
			if !ok {
				return fsapi.Invalid
			}
			r := Reference{e.Parent, e.Name, e.Revision}
			if !slices.Contains(o.References, r) {
				return fmt.Errorf("%w: entry lacks home reference", fsapi.Invalid)
			}
		}
	}
	for id, d := range s.retired {
		if !id.Valid() || id.Home != s.config.Volume || d.Object != id || !d.Creation.Valid() || d.Kind < fsapi.Regular || d.Kind > fsapi.Symlink {
			return fsapi.Invalid
		}
		if _, ok := s.objects[id]; ok {
			return fsapi.Invalid
		}
	}
	for _, p := range s.pins {
		if !p.ID.Valid() || !p.Object.Valid() || !p.Generation.Valid() || !p.Incarnation.Valid() {
			return fsapi.Invalid
		}
		if p.Active {
			if _, ok := s.objects[p.Object]; !ok {
				return fsapi.Invalid
			}
		}
	}
	return nil
}

func validateObject(o Object, volume fsapi.UUID) error {
	a := o.Attr
	d := o.Descriptor
	if a.Validate() != nil || a.Object.Home != volume || d.Object != a.Object || d.Kind != a.Kind || !d.Creation.Valid() || o.LedgerRevision == 0 {
		return fsapi.Invalid
	}
	if a.Kind != fsapi.Symlink && len(o.Text) != 0 || a.Kind == fsapi.Symlink && (len(o.Text) == 0 || len(o.Text) > 4096 || int64(len(o.Text)) != a.Size || bytes.IndexByte(o.Text, 0) >= 0) {
		return fsapi.Invalid
	}
	if a.Kind == fsapi.Directory {
		if o.Parent == nil || !o.Parent.Object.Valid() || o.Parent.Revision == 0 || a.Size != 0 {
			return fsapi.Invalid
		}
	} else if o.Parent != nil {
		return fsapi.Invalid
	}
	for i, r := range o.References {
		if !r.Parent.Valid() || !validName(r.Name) || r.Revision == 0 {
			return fsapi.Invalid
		}
		if i > 0 && compareRef(o.References[i-1], r) >= 0 {
			return fsapi.Invalid
		}
	}
	return nil
}

func (s *Store) operationHomes(op Operation) ([]fsapi.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	homes := map[fsapi.UUID]bool{s.config.Volume: true}
	add := func(id fsapi.ObjectID) {
		if id.Valid() {
			homes[id.Home] = true
		}
	}
	for _, change := range op.Objects {
		if old, ok := s.objects[change.After.Attr.Object]; ok {
			for _, r := range old.References {
				add(r.Parent)
			}
			if old.Parent != nil {
				add(old.Parent.Object)
			}
		}
	}
	add(op.Object)
	for _, o := range op.Objects {
		add(o.After.Attr.Object)
		for _, r := range o.After.References {
			add(r.Parent)
		}
		if o.After.Parent != nil {
			add(o.After.Parent.Object)
		}
	}
	for _, e := range op.Entries {
		add(e.After.Parent)
		if e.Before.Target != nil {
			add(*e.Before.Target)
		}
		if e.After.Target != nil {
			add(*e.After.Target)
		}
	}
	result := make([]fsapi.UUID, 0, len(homes))
	for h := range homes {
		result = append(result, h)
	}
	sort.Slice(result, func(i, j int) bool { return bytes.Compare(result[i][:], result[j][:]) < 0 })
	if len(op.Participants) != len(result) || !slices.Equal(op.Participants, result) {
		return nil, fmt.Errorf("%w: exact sorted participant set", fsapi.Invalid)
	}
	return result, nil
}
func (s *Store) applyPayload(ctx context.Context, op Operation) error {
	p := objectPath(op.Object) + "/payload"
	if err := s.checkPrivate(ctx, p, false); err != nil {
		return err
	}
	f, err := s.data.Open(ctx, p, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if op.Kind == Truncate {
		return f.Truncate(ctx, op.Size)
	}
	n, err := f.WriteAt(ctx, op.Data, op.Offset)
	if err != nil {
		return err
	}
	if n != len(op.Data) {
		return io.ErrShortWrite
	}
	return nil
}
func (s *Store) removePayload(ctx context.Context, id fsapi.ObjectID) error {
	o := s.objects[id]
	if o.Attr.Kind != fsapi.Regular {
		return nil
	}
	if err := s.checkPrivate(ctx, objectPath(id)+"/payload", false); err != nil {
		return err
	}
	if err := s.data.Unlink(ctx, objectPath(id)+"/payload"); err != nil {
		return err
	}
	return s.data.SyncDir(ctx, objectPath(id))
}
func (s *Store) readable(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.ready {
		return fsapi.Unavailable
	}
	return nil
}
func (s *Store) directory(id fsapi.ObjectID) error {
	o, ok := s.objects[id]
	if !ok {
		return fsapi.NotFound
	}
	if o.Attr.Kind != fsapi.Directory {
		return fsapi.NotDirectory
	}
	return nil
}
func (s *Store) hasForeign(id fsapi.ObjectID) bool {
	for _, e := range s.entries {
		if e.Parent == id && e.Target != nil && e.Target.Home != s.config.Volume {
			return true
		}
	}
	return false
}
func cloneEntry(e Entry) Entry {
	if e.Target != nil {
		v := *e.Target
		e.Target = &v
	}
	return e
}
func compareRef(a, b Reference) int {
	if c := lessID(a.Parent, b.Parent); c != 0 {
		return c
	}
	return bytes.Compare([]byte(a.Name), []byte(b.Name))
}

func cloneOperation(op Operation) Operation {
	op.Data = slices.Clone(op.Data)
	op.Participants = slices.Clone(op.Participants)
	op.Objects = slices.Clone(op.Objects)
	for i := range op.Objects {
		op.Objects[i].After = cloneObject(op.Objects[i].After)
	}
	op.Entries = slices.Clone(op.Entries)
	for i := range op.Entries {
		op.Entries[i].Before = cloneEntry(op.Entries[i].Before)
		op.Entries[i].After = cloneEntry(op.Entries[i].After)
	}
	op.Pins = slices.Clone(op.Pins)
	for i := range op.Pins {
		if op.Pins[i].Before != nil {
			p := *op.Pins[i].Before
			op.Pins[i].Before = &p
		}
	}
	return op
}
func operationBytes(op Operation) ([]byte, error) {
	op = cloneOperation(op)
	for i := range op.Objects {
		for j := range op.Objects[i].After.References {
			r := &op.Objects[i].After.References[j]
			r.Name = base64.StdEncoding.EncodeToString([]byte(r.Name))
		}
	}
	for i := range op.Entries {
		op.Entries[i].Before.Name = base64.StdEncoding.EncodeToString([]byte(op.Entries[i].Before.Name))
		op.Entries[i].After.Name = base64.StdEncoding.EncodeToString([]byte(op.Entries[i].After.Name))
	}
	return json.Marshal(op)
}
