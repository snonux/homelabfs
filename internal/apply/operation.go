package apply

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

func operation(ctx context.Context, s *storage.Store, h history, e segment.Entry, slot uint64) (*storage.Operation, error) {
	op := storage.Operation{ID: e.Record.Header.Operation, Position: e.Reference.Position}
	op.Position.Sequence = slot
	switch p := e.Record.Payload.(type) {
	case record.Commit:
		candidate, err := prior(h, e, p.Mutation)
		if err != nil {
			return nil, err
		}
		m, ok := candidate.Record.Payload.(record.Mutation)
		if !ok || candidate.Record.Header.Operation != e.Record.Header.Operation {
			return nil, fsapi.Invalid
		}
		op.ID = candidate.Record.Header.Operation
		if err = mutation(ctx, s, &op, m); err != nil {
			return nil, err
		}
	case record.Install:
		if p.Outcome != 1 || p.Prepare == nil || p.Redo == nil {
			return nil, fmt.Errorf("%w: only committed INSTALL has effects", fsapi.Invalid)
		}
		prepared, err := prior(h, e, p.Prepare.Prepare)
		if err != nil {
			return nil, err
		}
		prep, ok := prepared.Record.Payload.(record.Prepared)
		if !ok || p.Prepare.Participant != op.Position.Volume || prep.Request != p.Request || prep.Digest != p.Digest || !reflect.DeepEqual(prep.Binding, p.Binding) || !reflect.DeepEqual(prep.Operation.Redo, *p.Redo) || prep.RedoSHA256 != p.Prepare.RedoSHA256 || prepared.Record.Header.Operation != e.Record.Header.Operation {
			return nil, fsapi.Invalid
		}
		if err = mutation(ctx, s, &op, prep.Operation); err != nil {
			return nil, err
		}
	case record.HandlePin:
		op.Kind = storage.HandlePin
		op.Object = p.Object
		op.Pins = []storage.PinChange{{After: storage.Pin{ID: p.Pin, Object: p.Object, Generation: p.ServingGeneration, Incarnation: p.Incarnation, Active: true}}}
	case record.HandleRelease:
		image, err := s.Export(ctx)
		if err != nil {
			return nil, err
		}
		var old *storage.Pin
		for _, pin := range image.Pins {
			if pin.ID == p.Pin {
				old = &pin
				break
			}
		}
		if old == nil || old.Object != p.Object || old.Generation != p.ServingGeneration || old.Incarnation != p.Incarnation || !old.Active {
			return nil, fsapi.Stale
		}
		after := *old
		after.Active = false
		op.Kind = storage.HandleRelease
		op.Object = p.Object
		op.Pins = []storage.PinChange{{Before: old, After: after}}
	case record.Reclaim:
		op.Kind = storage.Reclaim
		op.Object = p.Object
		op.ExpectedRevision = p.ExpectedObjectRevision
		op.ExpectedLedgerRevision = p.ExpectedLedgerRevision
	default:
		return nil, fmt.Errorf("%w: selected record has no backing effect", fsapi.Invalid)
	}
	// An anchor-only participant may have no local redo. It still advances source
	// progress, but consumes no backing slot and cannot write a foreign payload.
	if op.Kind != storage.HandlePin && op.Kind != storage.HandleRelease && op.Kind != storage.Reclaim && len(op.Objects)+len(op.Entries) == 0 {
		if op.Kind != storage.Rename || op.Object.Home != op.Position.Volume {
			return nil, nil
		}
	}
	op.Participants = homes(ctx, s, op)
	return &op, nil
}
func prior(h history, e segment.Entry, ref record.FrameRef) (segment.Entry, error) {
	n := ref.Position.Sequence
	if n == 0 || n >= e.Reference.Position.Sequence || n > uint64(len(h.entries)) || h.entries[n-1].Reference != ref {
		return segment.Entry{}, fmt.Errorf("%w: exact prior dependency missing", fsapi.Stale)
	}
	return h.entries[n-1], nil
}
func mutation(ctx context.Context, s *storage.Store, op *storage.Operation, m record.Mutation) error {
	if err := arguments(op, m.Intent.Arguments); err != nil {
		return err
	}
	if a, ok := m.Intent.Arguments.(record.Rename); ok && a.Noop {
		if err := validateNoop(ctx, s, op.Position.Volume, a); err != nil {
			return err
		}
	}
	if err := entryIntent(*op, m.Intent.Arguments, m.Redo.Entries); err != nil {
		return err
	}
	changes := make(map[fsapi.ObjectID]int, len(m.Redo.Objects))
	for _, edit := range m.Redo.Objects {
		var obj storage.Object
		if edit.ExpectedRevision == 0 {
			obj = storage.Object{Descriptor: storage.Descriptor{Object: edit.Object, Kind: fsapi.Kind(edit.After.Type), Creation: op.ID}}
		} else {
			var err error
			obj, err = s.Stat(ctx, edit.Object)
			if err != nil {
				return err
			}
			if obj.Attr.Revision != edit.ExpectedRevision {
				return fsapi.Stale
			}
		}
		obj.Attr = metadata(edit.Object, edit.After)
		obj.Text = slices.Clone(edit.After.SymlinkText)
		changes[edit.Object] = len(op.Objects)
		op.Objects = append(op.Objects, storage.ObjectChange{Before: edit.ExpectedRevision, After: obj})
	}
	for _, edit := range m.Redo.Ledgers {
		index, ok := changes[edit.Object]
		if !ok {
			return fmt.Errorf("%w: ledger lacks metadata afterimage", fsapi.Invalid)
		}
		c := &op.Objects[index]
		obj := &c.After
		expectedNlink := uint64(0)
		if c.Before > 0 {
			old, err := s.Stat(ctx, edit.Object)
			if err != nil {
				return err
			}
			expectedNlink = old.Attr.Nlink
			if old.Attr.Kind == fsapi.Directory {
				expectedNlink = uint64(len(old.References))
			}
		}
		if obj.LedgerRevision != edit.ExpectedRevision || expectedNlink != edit.ExpectedNlink || obj.Attr.Kind != fsapi.Directory && obj.Attr.Nlink != edit.ResultingNlink {
			return fsapi.Stale
		}
		for _, r := range edit.Remove {
			ref := storage.Reference{Parent: r.Parent, Name: r.Name, Revision: r.EntryRevision}
			n := slices.Index(obj.References, ref)
			if n < 0 {
				return fsapi.Stale
			}
			obj.References = slices.Delete(obj.References, n, n+1)
		}
		for _, r := range edit.Add {
			for _, old := range obj.References {
				if old.Parent == r.Parent && old.Name == r.Name {
					return fsapi.Stale
				}
			}
			obj.References = append(obj.References, storage.Reference{Parent: r.Parent, Name: r.Name, Revision: r.EntryRevision})
		}
		sort.Slice(obj.References, func(i, j int) bool {
			a, b := obj.References[i], obj.References[j]
			if n := compareID(a.Parent, b.Parent); n != 0 {
				return n < 0
			}
			return a.Name < b.Name
		})
		if uint64(len(obj.References)) != edit.ResultingNlink {
			return fsapi.Stale
		}
		obj.LedgerRevision = edit.ResultingRevision
	}
	for _, edit := range m.Redo.Parents {
		index, ok := changes[edit.Directory]
		if !ok {
			return fsapi.Invalid
		}
		obj := &op.Objects[index].After
		var expected *storage.Parent
		if edit.Before != nil {
			expected = &storage.Parent{Revision: edit.Before.Revision, Object: edit.Before.Parent, EntryRevision: edit.Before.ParentEntryRevision}
		}
		if !reflect.DeepEqual(obj.Parent, expected) {
			return fsapi.Stale
		}
		obj.Parent = &storage.Parent{Revision: edit.After.Revision, Object: edit.After.Parent, EntryRevision: edit.After.ParentEntryRevision}
	}
	for _, edit := range m.Redo.Entries {
		op.Entries = append(op.Entries, storage.EntryChange{Before: storage.Entry{Parent: edit.Parent, Name: edit.Name, Revision: edit.Before.Revision, Target: edit.Before.Target}, After: storage.Entry{Parent: edit.Parent, Name: edit.Name, Revision: edit.After.Revision, Target: edit.After.Target}})
	}
	return nil
}
func arguments(op *storage.Operation, args any) error {
	switch a := args.(type) {
	case record.Create:
		op.Kind = storage.Create
		op.Object = a.Object
	case record.Mkdir:
		op.Kind = storage.Mkdir
		op.Object = a.Object
	case record.Symlink:
		op.Kind = storage.Symlink
		op.Object = a.Object
	case record.Link:
		op.Kind = storage.Link
		op.Object = a.Source
	case record.Unlink:
		op.Kind = storage.Unlink
		op.Object = a.Removed
	case record.Rmdir:
		op.Kind = storage.Rmdir
		op.Object = a.Removed
	case record.Rename:
		op.Kind = storage.Rename
		op.Object = a.Source
	case record.Write:
		op.Kind = storage.Write
		op.Object = a.Object
		op.Offset = int64(a.Offset)
		op.Data = slices.Clone(a.Data)
	case record.Truncate:
		op.Kind = storage.Truncate
		op.Object = a.Object
		op.Size = int64(a.Size)
	case record.Setattr:
		op.Kind = storage.Setattr
		op.Object = a.Object
	default:
		return fsapi.Invalid
	}
	return nil
}
func metadata(id fsapi.ObjectID, m record.Metadata) fsapi.Attr {
	return fsapi.Attr{Object: id, Kind: fsapi.Kind(m.Type), UID: m.UID, GID: m.GID, Mode: m.Mode, Size: int64(m.Size), Nlink: m.Nlink, Revision: m.Revision, Atime: m.Atime, Mtime: m.Mtime, Ctime: m.Ctime}
}
func compareID(a, b fsapi.ObjectID) int {
	if n := bytes.Compare(a.Home[:], b.Home[:]); n != 0 {
		return n
	}
	return bytes.Compare(a.Allocation[:], b.Allocation[:])
}
func homes(ctx context.Context, s *storage.Store, op storage.Operation) []fsapi.UUID {
	set := map[fsapi.UUID]bool{op.Position.Volume: true}
	add := func(id fsapi.ObjectID) {
		if id.Valid() {
			set[id.Home] = true
		}
	}
	object := func(o storage.Object) {
		add(o.Attr.Object)
		for _, r := range o.References {
			add(r.Parent)
		}
		if o.Parent != nil {
			add(o.Parent.Object)
		}
	}
	add(op.Object)
	for _, c := range op.Objects {
		object(c.After)
		if old, err := s.Stat(ctx, c.After.Attr.Object); err == nil {
			object(old)
		}
	}
	for _, c := range op.Entries {
		add(c.After.Parent)
		if c.Before.Target != nil {
			add(*c.Before.Target)
		}
		if c.After.Target != nil {
			add(*c.After.Target)
		}
	}
	out := make([]fsapi.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}
func entryIntent(op storage.Operation, args any, edits []record.EntryEdit) error {
	// Home fragments contain a subset, but every included edit must name the exact
	// protected parent/name and chosen source/replacement identity.
	for _, e := range edits {
		target := func(id fsapi.ObjectID) bool { return e.After.Target != nil && *e.After.Target == id }
		removed := func(id fsapi.ObjectID) bool {
			return e.Before.Target != nil && *e.Before.Target == id && e.After.Target == nil
		}
		valid := false
		switch a := args.(type) {
		case record.Create:
			valid = e.Parent == a.Parent && e.Name == a.Name && e.Before.Target == nil && target(a.Object)
		case record.Mkdir:
			valid = e.Parent == a.Parent && e.Name == a.Name && e.Before.Target == nil && target(a.Object)
		case record.Symlink:
			valid = e.Parent == a.Parent && e.Name == a.Name && e.Before.Target == nil && target(a.Object)
		case record.Link:
			valid = e.Parent == a.TargetParent && e.Name == a.TargetName && e.Before.Target == nil && target(a.Source)
		case record.Unlink:
			valid = e.Parent == a.Parent && e.Name == a.Name && removed(a.Removed)
		case record.Rmdir:
			valid = e.Parent == a.Parent && e.Name == a.Name && removed(a.Removed)
		case record.Rename:
			valid = !a.Noop && (e.Parent == a.SourceParent && e.Name == a.SourceName && removed(a.Source) || e.Parent == a.DestinationParent && e.Name == a.DestinationName && target(a.Source) && reflect.DeepEqual(e.Before.Target, a.Replaced))
		}
		if !valid {
			return fmt.Errorf("%w: intent/name redo mismatch for kind %d", fsapi.Invalid, op.Kind)
		}
	}
	return nil
}

func validateNoop(ctx context.Context, s *storage.Store, volume fsapi.UUID, a record.Rename) error {
	if a.Source.Home == volume {
		o, err := s.Stat(ctx, a.Source)
		if err != nil {
			return err
		}
		if o.Attr.Kind == fsapi.Directory && (a.SourceParent != a.DestinationParent || a.SourceName != a.DestinationName) {
			return fsapi.Invalid
		}
	}
	for _, name := range []struct {
		parent fsapi.ObjectID
		name   string
	}{{a.SourceParent, a.SourceName}, {a.DestinationParent, a.DestinationName}} {
		if name.parent.Home == volume {
			e, err := s.Lookup(ctx, name.parent, name.name)
			if err != nil {
				return err
			}
			if e.Target == nil || *e.Target != a.Source {
				return fsapi.Stale
			}
		}
	}
	return nil
}
