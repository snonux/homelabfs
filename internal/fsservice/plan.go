package fsservice

import (
	"bytes"
	"math"
	"slices"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

type nameKey struct {
	parent fsapi.ObjectID
	name   string
}
type plan struct {
	authorizations []record.Authorization
	image          storage.Image
	objects        map[fsapi.ObjectID]storage.Object
	before         map[fsapi.ObjectID]storage.Object
	changed        map[fsapi.ObjectID]storage.Object
	entries        map[nameKey]storage.Entry
	edits          []record.EntryEdit
	now            fsapi.Timestamp
}

func newPlan(image storage.Image, now fsapi.Timestamp) *plan {
	p := &plan{image: image, objects: make(map[fsapi.ObjectID]storage.Object), before: make(map[fsapi.ObjectID]storage.Object), changed: make(map[fsapi.ObjectID]storage.Object), entries: make(map[nameKey]storage.Entry), now: now}
	for _, o := range image.Objects {
		p.objects[o.Attr.Object] = o
	}
	for _, e := range image.Entries {
		p.entries[nameKey{e.Parent, e.Name}] = e
	}
	return p
}
func (p *plan) object(id fsapi.ObjectID) (storage.Object, error) {
	o, ok := p.objects[id]
	if !ok {
		return o, fsapi.NotFound
	}
	return o, nil
}
func (p *plan) directory(id fsapi.ObjectID) (storage.Object, error) {
	o, err := p.object(id)
	if err != nil {
		return o, err
	}
	if o.Attr.Kind != fsapi.Directory {
		return o, fsapi.NotDirectory
	}
	if o.Attr.Nlink == 0 {
		return o, fsapi.NotFound
	}
	return o, nil
}
func (p *plan) entry(parent fsapi.ObjectID, name string) storage.Entry {
	if e, ok := p.entries[nameKey{parent, name}]; ok {
		return e
	}
	return storage.Entry{Parent: parent, Name: name}
}
func (p *plan) touch(id fsapi.ObjectID) (storage.Object, error) {
	if o, ok := p.changed[id]; ok {
		return o, nil
	}
	old, err := p.object(id)
	if err != nil {
		return old, err
	}
	if old.Attr.Revision == math.MaxUint64 {
		return old, fsapi.NoSpace
	}
	p.before[id] = old
	o := old
	o.References = slices.Clone(old.References)
	if old.Parent != nil {
		v := *old.Parent
		o.Parent = &v
	}
	o.Attr.Revision++
	o.Attr.Ctime = p.now
	p.save(o)
	return o, nil
}
func (p *plan) save(o storage.Object) { p.changed[o.Attr.Object] = o; p.objects[o.Attr.Object] = o }
func (p *plan) alterName(parent fsapi.ObjectID, name string, target *fsapi.ObjectID) error {
	old := p.entry(parent, name)
	if old.Revision == math.MaxUint64 {
		return fsapi.NoSpace
	}
	after := storage.Entry{Parent: parent, Name: name, Revision: old.Revision + 1, Target: target}
	if old.Target != nil {
		if err := p.reference(*old.Target, storage.Reference{Parent: parent, Name: name, Revision: old.Revision}, false); err != nil {
			return err
		}
	}
	if target != nil {
		if err := p.reference(*target, storage.Reference{Parent: parent, Name: name, Revision: after.Revision}, true); err != nil {
			return err
		}
	}
	par, err := p.touch(parent)
	if err != nil {
		return err
	}
	par.Attr.Mtime = p.now
	p.save(par)
	p.entries[nameKey{parent, name}] = after
	p.edits = append(p.edits, record.EntryEdit{Parent: parent, Name: name, Before: record.EntryState{Revision: old.Revision, Target: old.Target}, After: record.EntryState{Revision: after.Revision, Target: target}})
	return nil
}
func (p *plan) reference(id fsapi.ObjectID, ref storage.Reference, add bool) error {
	o, err := p.touch(id)
	if err != nil {
		return err
	}
	before := p.before[id]
	if o.LedgerRevision == before.LedgerRevision {
		if o.LedgerRevision == math.MaxUint64 {
			return fsapi.NoSpace
		}
		o.LedgerRevision++
	}
	if add {
		o.References = append(o.References, ref)
	} else {
		n := slices.Index(o.References, ref)
		if n < 0 {
			return fsapi.Stale
		}
		o.References = slices.Delete(o.References, n, n+1)
	}
	if o.Attr.Kind != fsapi.Directory {
		o.Attr.Nlink = uint64(len(o.References))
	} else if add {
		revision := uint64(1)
		if before.Parent != nil {
			if before.Parent.Revision == math.MaxUint64 {
				return fsapi.NoSpace
			}
			revision = before.Parent.Revision + 1
		}
		o.Parent = &storage.Parent{Revision: revision, Object: ref.Parent, EntryRevision: ref.Revision}
	}
	p.save(o)
	return nil
}
func (p *plan) dirLinks(parent fsapi.ObjectID, delta int) error {
	o, err := p.touch(parent)
	if err != nil {
		return err
	}
	if delta < 0 {
		if o.Attr.Nlink < uint64(-delta) {
			return fsapi.Invalid
		}
		o.Attr.Nlink -= uint64(-delta)
	} else {
		if o.Attr.Nlink > math.MaxUint64-uint64(delta) {
			return fsapi.NoSpace
		}
		o.Attr.Nlink += uint64(delta)
	}
	p.save(o)
	return nil
}
func (p *plan) empty(id fsapi.ObjectID) bool {
	for _, e := range p.entries {
		if e.Parent == id && e.Target != nil {
			return false
		}
	}
	return true
}
func (p *plan) finish(kind uint16, args any) (record.Mutation, error) {
	redo := record.Redo{Entries: p.edits}
	for id, o := range p.changed {
		old := p.before[id]
		// New objects are touched while attaching their first name; their selected
		// revision is one, not an update of the unpublished temporary descriptor.
		if old.Attr.Object == (fsapi.ObjectID{}) {
			o.Attr.Revision = 1
			p.save(o)
		}
		edit := record.ObjectEdit{Object: id, ExpectedRevision: old.Attr.Revision, After: metadata(o)}
		redo.Objects = append(redo.Objects, edit)
		if o.LedgerRevision != old.LedgerRevision {
			ledger := record.LedgerEdit{Object: id, ExpectedRevision: old.LedgerRevision, ResultingRevision: o.LedgerRevision, ExpectedNlink: uint64(len(old.References)), ResultingNlink: uint64(len(o.References))}
			for _, r := range old.References {
				if !slices.Contains(o.References, r) {
					ledger.Remove = append(ledger.Remove, reference(r))
				}
			}
			for _, r := range o.References {
				if !slices.Contains(old.References, r) {
					ledger.Add = append(ledger.Add, reference(r))
				}
			}
			sortReferences(ledger.Remove)
			sortReferences(ledger.Add)
			redo.Ledgers = append(redo.Ledgers, ledger)
		}
		if o.Parent != nil && (old.Parent == nil || *old.Parent != *o.Parent) {
			e := record.ParentEdit{Directory: id, After: parent(*o.Parent)}
			if old.Parent != nil {
				v := parent(*old.Parent)
				e.Before = &v
			}
			redo.Parents = append(redo.Parents, e)
		}
	}
	sort.Slice(redo.Objects, func(i, j int) bool { return compareID(redo.Objects[i].Object, redo.Objects[j].Object) < 0 })
	sort.Slice(redo.Ledgers, func(i, j int) bool { return compareID(redo.Ledgers[i].Object, redo.Ledgers[j].Object) < 0 })
	sort.Slice(redo.Parents, func(i, j int) bool { return compareID(redo.Parents[i].Directory, redo.Parents[j].Directory) < 0 })
	sort.Slice(redo.Entries, func(i, j int) bool {
		a, b := redo.Entries[i], redo.Entries[j]
		if n := compareID(a.Parent, b.Parent); n != 0 {
			return n < 0
		}
		return wireNameLess(a.Name, b.Name)
	})
	return record.Mutation{Intent: record.Intent{Kind: kind, Arguments: args, SelectedMetadata: slices.Clone(redo.Objects)}, Redo: redo, Authorizations: p.authorizations}, nil
}
func metadata(o storage.Object) record.Metadata {
	a := o.Attr
	return record.Metadata{Revision: a.Revision, Type: uint8(a.Kind), UID: a.UID, GID: a.GID, Mode: a.Mode, Size: uint64(a.Size), Nlink: a.Nlink, Atime: a.Atime, Mtime: a.Mtime, Ctime: a.Ctime, SymlinkText: slices.Clone(o.Text)}
}
func parent(p storage.Parent) record.ParentState {
	return record.ParentState{Revision: p.Revision, Parent: p.Object, ParentEntryRevision: p.EntryRevision}
}
func reference(r storage.Reference) record.Reference {
	return record.Reference{Parent: r.Parent, Name: r.Name, EntryRevision: r.Revision}
}
func compareID(a, b fsapi.ObjectID) int {
	if n := bytes.Compare(a.Home[:], b.Home[:]); n != 0 {
		return n
	}
	return bytes.Compare(a.Allocation[:], b.Allocation[:])
}
func wireNameLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
func sortReferences(refs []record.Reference) {
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if n := compareID(a.Parent, b.Parent); n != 0 {
			return n < 0
		}
		return wireNameLess(a.Name, b.Name)
	})
}
