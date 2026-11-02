package fsservice

import (
	"context"
	"math"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

type preparedMutation struct {
	Result    fsapi.Result
	Object    fsapi.ObjectID
	Operation fsapi.UUID
	Mutation  *record.Mutation
}
type planningConfig struct {
	root          fsapi.ObjectID
	storageLimits storage.Limits
	logicalBytes  int64
	identities    func(context.Context, bool) (fsapi.UUID, fsapi.ObjectID, error)
}

func (s *Service) mutate(ctx context.Context, cred fsapi.Cred, c fsapi.Command, p *plan) (fsapi.Result, error) {
	if c.Operation == fsapi.Create || c.Operation == fsapi.Mkdir || c.Operation == fsapi.SymlinkOp {
		if (c.Operation == fsapi.Create && len(s.handles) >= s.config.Limits.Handles) || s.lookupCount >= uint64(s.config.Limits.Lookups) {
			return fsapi.Result{}, fsapi.NoSpace
		}
		if entry := p.entry(c.Parent, c.Name); entry.Target != nil && c.Operation == fsapi.Create && !c.Flags.Exclusive {
			return s.openExisting(ctx, cred, c, p, *entry.Target)
		}
	}
	config := planningConfig{root: s.config.Storage.Root, storageLimits: s.config.Storage.Limits, logicalBytes: s.config.Limits.LogicalBytes, identities: func(ctx context.Context, creation bool) (fsapi.UUID, fsapi.ObjectID, error) {
		op, err := s.newUUID()
		if err != nil {
			return op, fsapi.ObjectID{}, err
		}
		if !creation {
			return op, fsapi.ObjectID{}, nil
		}
		id, err := s.reserve(ctx, op)
		return op, id, err
	}}
	prepared, err := prepareMutation(ctx, cred, c, p, config)
	if err != nil {
		return fsapi.Result{}, err
	}
	result, id := prepared.Result, prepared.Object
	if prepared.Mutation == nil {
		result.Position = s.selection.Through.Position
		return result, nil
	}
	if err = s.protect(ctx, prepared.Operation, *prepared.Mutation); err != nil {
		return result, err
	}

	if c.Operation == fsapi.Create || c.Operation == fsapi.Mkdir || c.Operation == fsapi.SymlinkOp {
		var flags *fsapi.OpenFlags
		if c.Operation == fsapi.Create {
			flags = &c.Flags
		}
		if err = s.acquire(ctx, id, true, flags); err != nil {
			return result, err
		}
		if flags != nil {
			result.Handle = s.nextHandle
		}
	}
	if id.Valid() {
		o, e := s.applied.Store.Stat(ctx, id)
		if e != nil {
			return result, e
		}
		result.Attr = &o.Attr
	}
	if c.Operation == fsapi.Unlink || c.Operation == fsapi.Rmdir || c.Operation == fsapi.RenameOp {
		if err = s.reap(ctx); err != nil {
			return result, err
		}
	}
	result.Position = s.selection.Through.Position
	return result, nil
}
func (s *Service) openExisting(ctx context.Context, cred fsapi.Cred, c fsapi.Command, p *plan, id fsapi.ObjectID) (fsapi.Result, error) {
	if err := c.Flags.Validate(); err != nil {
		return fsapi.Result{}, err
	}
	if !c.Flags.Create {
		return fsapi.Result{}, fsapi.Invalid
	}
	object, err := p.object(id)
	if err != nil {
		return fsapi.Result{}, err
	}
	if object.Attr.Kind == fsapi.Directory {
		return fsapi.Result{}, fsapi.IsDirectory
	}
	if object.Attr.Kind != fsapi.Regular {
		return fsapi.Result{}, fsapi.Unsupported
	}
	if c.Flags.Truncate {
		tc := c
		tc.Operation = fsapi.Truncate
		tc.Object = id
		tc.Size = 0
		if _, err = s.mutate(ctx, cred, tc, p); err != nil {
			return fsapi.Result{}, err
		}
		object, err = s.applied.Store.Stat(ctx, id)
		if err != nil {
			return fsapi.Result{}, err
		}
	}
	if err = s.acquire(ctx, id, true, &c.Flags); err != nil {
		return fsapi.Result{}, err
	}
	return fsapi.Result{Attr: &object.Attr, Handle: s.nextHandle, Position: s.selection.Through.Position}, nil
}

func prepareMutation(ctx context.Context, cred fsapi.Cred, c fsapi.Command, p *plan, config planningConfig) (preparedMutation, error) {
	result := fsapi.Result{}
	var kind uint16
	var args any
	var id fsapi.ObjectID
	var op fsapi.UUID
	var err error
	switch c.Operation {
	case fsapi.Create, fsapi.Mkdir, fsapi.SymlinkOp:
		if err = validName(c.Name); err != nil {
			return preparedMutation{}, err
		}
		par, e := p.directory(c.Parent)
		if e != nil {
			return preparedMutation{}, e
		}
		existing := p.entry(c.Parent, c.Name)
		if c.Operation == fsapi.Create {
			if err = c.Flags.Validate(); err != nil {
				return preparedMutation{}, err
			}
			if !c.Flags.Create {
				return preparedMutation{}, fsapi.Invalid
			}
		}
		if existing.Target != nil {
			return preparedMutation{}, fsapi.Exists
		}
		if c.Mode & ^uint16(0o7777) != 0 || c.Umask & ^uint16(0o777) != 0 {
			return preparedMutation{}, fsapi.Invalid
		}
		if c.Operation == fsapi.SymlinkOp && (len(c.Data) == 0 || len(c.Data) > 4096 || slices.Contains(c.Data, byte(0))) {
			return preparedMutation{}, fsapi.Invalid
		}
		op, id, err = config.identities(ctx, true)
		if err != nil {
			return preparedMutation{}, err
		}
		o := storage.Object{Descriptor: storage.Descriptor{Object: id, Kind: fsapi.Regular, Creation: op}, Attr: fsapi.Attr{Object: id, Kind: fsapi.Regular, UID: cred.UID, GID: par.Attr.GID, Mode: c.Mode & ^c.Umask, Nlink: 1, Atime: p.now, Mtime: p.now, Ctime: p.now}}
		kind = 1
		args = record.Create{Parent: c.Parent, Name: c.Name, Object: id, Exclusive: c.Flags.Exclusive, RequestedMode: c.Mode, Umask: c.Umask}
		if c.Operation == fsapi.Mkdir {
			o.Descriptor.Kind = fsapi.Directory
			o.Attr.Kind = fsapi.Directory
			o.Attr.Nlink = 2
			if par.Attr.Mode&0o2000 != 0 {
				o.Attr.Mode |= 0o2000
			}
			kind = 2
			args = record.Mkdir{Parent: c.Parent, Name: c.Name, Object: id, RequestedMode: c.Mode, Umask: c.Umask}
		}
		if c.Operation == fsapi.SymlinkOp {
			o.Descriptor.Kind = fsapi.Symlink
			o.Attr.Kind = fsapi.Symlink
			o.Attr.Mode = 0o777
			o.Attr.Size = int64(len(c.Data))
			o.Text = slices.Clone(c.Data)
			kind = 3
			args = record.Symlink{Parent: c.Parent, Name: c.Name, Object: id, Text: slices.Clone(c.Data)}
		}
		p.objects[id] = o
		if err = p.alterName(c.Parent, c.Name, &id); err != nil {
			return preparedMutation{}, err
		}
		if c.Operation == fsapi.Mkdir {
			if err = p.dirLinks(c.Parent, 1); err != nil {
				return preparedMutation{}, err
			}
		}
	case fsapi.Write, fsapi.Truncate, fsapi.Setattr:
		id = c.Object
		o, e := p.object(id)
		if e != nil {
			return preparedMutation{}, e
		}
		if c.Operation != fsapi.Setattr && o.Attr.Kind != fsapi.Regular {
			if o.Attr.Kind == fsapi.Directory {
				return preparedMutation{}, fsapi.IsDirectory
			}
			return preparedMutation{}, fsapi.Invalid
		}
		if c.Operation == fsapi.Write {
			if c.Handle == 0 {
				return preparedMutation{}, fsapi.Stale
			}
			if err = fsapi.ValidateRange(c.Offset, int64(len(c.Data))); err != nil {
				return preparedMutation{}, err
			}
			if len(c.Data) == 0 {
				return preparedMutation{Result: fsapi.Result{}}, nil
			}
		}
		o, err = p.touch(id)
		if err != nil {
			return preparedMutation{}, err
		}
		switch c.Operation {
		case fsapi.Write:
			o.Attr.Size = max(o.Attr.Size, c.Offset+int64(len(c.Data)))
			o.Attr.Mtime = p.now
			if cred.UID != 0 {
				o.Attr.Mode &^= 0o6000
			}
			kind = 8
			args = record.Write{Object: id, Offset: uint64(c.Offset), Data: slices.Clone(c.Data)}
			result.Count = int64(len(c.Data))
		case fsapi.Truncate:
			if c.Size < 0 {
				return preparedMutation{}, fsapi.Invalid
			}
			if c.Attributes != (fsapi.Attributes{}) {
				return preparedMutation{}, fsapi.Unsupported
			}
			o.Attr.Size = c.Size
			o.Attr.Mtime = p.now
			o, err = truncateTimes(o, c.TruncateTimes, p.now)
			if err != nil {
				return preparedMutation{}, err
			}
			if cred.UID != 0 {
				o.Attr.Mode &^= 0o6000
			}
			kind = 9
			args = record.Truncate{Object: id, Size: uint64(c.Size)}
		case fsapi.Setattr:
			var a record.Setattr
			o, a, err = attributes(o, c.Attributes, p.now)
			if err != nil {
				return preparedMutation{}, err
			}
			kind = 10
			args = a
		}
		p.save(o)
	case fsapi.LinkOp:
		if err = validName(c.NewName); err != nil {
			return preparedMutation{}, err
		}
		if _, err = p.directory(c.Destination); err != nil {
			return preparedMutation{}, err
		}
		o, e := p.object(c.Object)
		if e != nil {
			return preparedMutation{}, e
		}
		if o.Attr.Kind != fsapi.Regular {
			return preparedMutation{}, fsapi.Unsupported
		}
		if o.Attr.Nlink == 0 {
			return preparedMutation{}, fsapi.NotFound
		}
		if o.Attr.Nlink >= 4096 {
			return preparedMutation{}, fsapi.TooManyLinks
		}
		if p.entry(c.Destination, c.NewName).Target != nil {
			return preparedMutation{}, fsapi.Exists
		}
		id = c.Object
		if err = p.alterName(c.Destination, c.NewName, &id); err != nil {
			return preparedMutation{}, err
		}
		kind = 4
		args = record.Link{Source: id, TargetParent: c.Destination, TargetName: c.NewName}
	case fsapi.Unlink, fsapi.Rmdir:
		if err = validName(c.Name); err != nil {
			return preparedMutation{}, err
		}
		if _, err = p.directory(c.Parent); err != nil {
			return preparedMutation{}, err
		}
		entry := p.entry(c.Parent, c.Name)
		if entry.Target == nil {
			return preparedMutation{}, fsapi.NotFound
		}
		id = *entry.Target
		o, e := p.object(id)
		if e != nil {
			return preparedMutation{}, e
		}
		if (o.Attr.Kind == fsapi.Directory) != (c.Operation == fsapi.Rmdir) {
			if o.Attr.Kind == fsapi.Directory {
				return preparedMutation{}, fsapi.IsDirectory
			}
			return preparedMutation{}, fsapi.NotDirectory
		}
		if id == config.root {
			return preparedMutation{}, fsapi.Busy
		}
		if c.Operation == fsapi.Rmdir && !p.empty(id) {
			return preparedMutation{}, fsapi.NotEmpty
		}
		if err = p.alterName(c.Parent, c.Name, nil); err != nil {
			return preparedMutation{}, err
		}
		if c.Operation == fsapi.Rmdir {
			o = p.objects[id]
			o.Attr.Nlink = 0
			p.save(o)
			if err = p.dirLinks(c.Parent, -1); err != nil {
				return preparedMutation{}, err
			}
			kind = 6
			args = record.Rmdir{Parent: c.Parent, Name: c.Name, Removed: id}
		} else {
			kind = 5
			args = record.Unlink{Parent: c.Parent, Name: c.Name, Removed: id}
		}
	case fsapi.RenameOp:
		args, id, err = rename(c, p, config.root)
		if err != nil {
			return preparedMutation{}, err
		}
		kind = 7
	default:
		return preparedMutation{}, fsapi.Unsupported
	}
	if err = admitPlan(p, config.storageLimits, config.logicalBytes); err != nil {
		return preparedMutation{}, err
	}
	if !op.Valid() {
		op, _, err = config.identities(ctx, false)
		if err != nil {
			return preparedMutation{}, err
		}
	}
	mutation, err := p.finish(kind, args)
	if err != nil {
		return preparedMutation{}, err
	}
	return preparedMutation{Result: result, Object: id, Operation: op, Mutation: &mutation}, nil
}
func truncateTimes(o storage.Object, times fsapi.TruncateTimes, now fsapi.Timestamp) (storage.Object, error) {
	for _, t := range []struct {
		value *fsapi.Timestamp
		now   bool
		out   *fsapi.Timestamp
	}{{times.Atime, times.AtimeNow, &o.Attr.Atime}, {times.Mtime, times.MtimeNow, &o.Attr.Mtime}, {times.Ctime, false, &o.Attr.Ctime}} {
		if t.value != nil {
			if t.now || !t.value.Valid() {
				return o, fsapi.Invalid
			}
			*t.out = *t.value
		} else if t.now {
			*t.out = now
		}
	}
	return o, nil
}
func rename(c fsapi.Command, p *plan, root fsapi.ObjectID) (record.Rename, fsapi.ObjectID, error) {
	a := record.Rename{SourceParent: c.Parent, SourceName: c.Name, DestinationParent: c.Destination, DestinationName: c.NewName, Policy: 1}
	if c.NoReplace {
		a.Policy = 2
	}
	if err := validName(c.Name); err != nil {
		return a, a.Source, err
	}
	if err := validName(c.NewName); err != nil {
		return a, a.Source, err
	}
	if _, err := p.directory(c.Parent); err != nil {
		return a, a.Source, err
	}
	if _, err := p.directory(c.Destination); err != nil {
		return a, a.Source, err
	}
	source := p.entry(c.Parent, c.Name)
	dest := p.entry(c.Destination, c.NewName)
	if source.Target == nil {
		return a, a.Source, fsapi.NotFound
	}
	a.Source = *source.Target
	a.Replaced = dest.Target
	obj, err := p.object(a.Source)
	if err != nil {
		return a, a.Source, err
	}
	if a.Source == root {
		return a, a.Source, fsapi.Busy
	}
	if dest.Target != nil && a.Policy == 2 {
		return a, a.Source, fsapi.Exists
	}
	if dest.Target != nil && *dest.Target == a.Source {
		a.Noop = true
		return a, a.Source, nil
	}
	if obj.Attr.Kind == fsapi.Directory {
		cur := c.Destination
		for steps := 0; cur != root; steps++ {
			if cur == a.Source {
				return a, a.Source, fsapi.Invalid
			}
			if steps > len(p.objects) {
				return a, a.Source, fsapi.Invalid
			}
			o, e := p.directory(cur)
			if e != nil {
				return a, a.Source, e
			}
			cur = o.Parent.Object
		}
	}
	if dest.Target != nil {
		replaced, e := p.object(*dest.Target)
		if e != nil {
			return a, a.Source, e
		}
		if (obj.Attr.Kind == fsapi.Directory) != (replaced.Attr.Kind == fsapi.Directory) {
			if obj.Attr.Kind == fsapi.Directory {
				return a, a.Source, fsapi.NotDirectory
			}
			return a, a.Source, fsapi.IsDirectory
		}
		if replaced.Attr.Kind == fsapi.Directory && !p.empty(*dest.Target) {
			return a, a.Source, fsapi.NotEmpty
		}
	}
	if err = p.alterName(c.Parent, c.Name, nil); err != nil {
		return a, a.Source, err
	}
	if err = p.alterName(c.Destination, c.NewName, &a.Source); err != nil {
		return a, a.Source, err
	}
	if obj.Attr.Kind == fsapi.Directory {
		if err = p.dirLinks(c.Parent, -1); err != nil {
			return a, a.Source, err
		}
		if err = p.dirLinks(c.Destination, 1); err != nil {
			return a, a.Source, err
		}
	}
	if dest.Target != nil && p.objects[*dest.Target].Attr.Kind == fsapi.Directory {
		o := p.objects[*dest.Target]
		o.Attr.Nlink = 0
		p.save(o)
		if err = p.dirLinks(c.Destination, -1); err != nil {
			return a, a.Source, err
		}
	}
	return a, a.Source, nil
}
func attributes(o storage.Object, a fsapi.Attributes, now fsapi.Timestamp) (storage.Object, record.Setattr, error) {
	r := record.Setattr{Object: o.Attr.Object}
	if a.Mode != nil && (*a.Mode&0o6000) != 0 && (a.UID != nil || a.GID != nil) {
		return o, r, fsapi.Unsupported
	}
	if a.Mode != nil {
		if *a.Mode & ^uint16(0o7777) != 0 {
			return o, r, fsapi.Invalid
		}
		r.Mask |= 1
		r.Mode = a.Mode
		o.Attr.Mode = *a.Mode
	}
	if a.UID != nil {
		r.Mask |= 2
		r.UID = a.UID
		o.Attr.UID = *a.UID
		o.Attr.Mode &^= 0o6000
	}
	if a.GID != nil {
		r.Mask |= 4
		r.GID = a.GID
		o.Attr.GID = *a.GID
		o.Attr.Mode &^= 0o6000
	}
	for i, t := range []struct {
		value *fsapi.Timestamp
		now   bool
	}{{a.Atime, a.AtimeNow}, {a.Mtime, a.MtimeNow}} {
		if t.value != nil && t.now {
			return o, r, fsapi.Invalid
		}
		if t.value == nil && !t.now {
			continue
		}
		selected := record.TimeSelection{Source: 1}
		if t.now {
			selected.Source = 2
			selected.Value = now
		} else {
			selected.Value = *t.value
		}
		if !selected.Value.Valid() {
			return o, r, fsapi.Invalid
		}
		if i == 0 {
			r.Mask |= 8
			r.Atime = &selected
			o.Attr.Atime = selected.Value
		} else {
			r.Mask |= 16
			r.Mtime = &selected
			o.Attr.Mtime = selected.Value
		}
	}
	if r.Mask == 0 {
		return o, r, fsapi.Invalid
	}
	return o, r, nil
}
func (s *Service) admitPlan(p *plan) error {
	return admitPlan(p, s.config.Storage.Limits, s.config.Limits.LogicalBytes)
}
func admitPlan(p *plan, l storage.Limits, logicalBytes int64) error {
	objects, entries, operations := l.Objects, l.Entries, l.Operations
	if objects == 0 {
		objects = 4096
	}
	if entries == 0 {
		entries = 16384
	}
	if operations == 0 {
		operations = 16384
	}
	if len(p.objects)+len(p.image.Retired) > objects || len(p.entries) > entries || len(p.image.Receipts) >= operations {
		return fsapi.NoSpace
	}
	var size int64
	for _, o := range p.objects {
		if o.Attr.Size > math.MaxInt64-size || o.Attr.Size > logicalBytes-size {
			return fsapi.NoSpace
		}
		size += o.Attr.Size
	}
	return nil
}
