package fsservice

import (
	"context"
	"errors"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

func (s *Service) execute(ctx context.Context, cred fsapi.Cred, c fsapi.Command) (fsapi.Result, error) {
	original := c.Clone()
	if err := s.check(ctx); err != nil {
		return fsapi.Result{}, s.block(err)
	}
	if c.Mount != s.config.Storage.Volume {
		return fsapi.Result{}, fsapi.CrossDevice
	}
	if (c.Operation == fsapi.RenameOp || c.Operation == fsapi.LinkOp) && c.DestinationMount != c.Mount {
		return fsapi.Result{}, fsapi.CrossDevice
	}
	for _, id := range []fsapi.ObjectID{c.Object, c.Parent, c.Destination} {
		if id.Valid() && id.Home != c.Mount {
			return fsapi.Result{}, fsapi.Unsupported
		}
	}
	if c.Required != (fsapi.Position{}) {
		p := s.selection.Through.Position
		required := c.Required
		p.Sequence = 0
		required.Sequence = 0
		if p != required {
			return fsapi.Result{}, fsapi.Stale
		}
		if c.Required.Sequence > s.selection.Through.Position.Sequence {
			return fsapi.Result{}, fsapi.Unavailable
		}
	}
	if (c.Generation.Valid() && c.Generation != s.config.Workspace.ID) || ((c.Handle != 0 || c.DirectoryRevision != 0) && !c.Generation.Valid()) {
		return fsapi.Result{}, fsapi.Stale
	}
	if c.Handle != 0 {
		h, ok := s.handles[c.Handle]
		if !ok {
			return fsapi.Result{}, fsapi.Stale
		}
		if c.Object.Valid() && c.Object != h.object {
			return fsapi.Result{}, fsapi.Stale
		}
		c.Object = h.object
		if c.Operation == fsapi.Read && h.flags.Access == fsapi.WriteOnly || (c.Operation == fsapi.Write || c.Operation == fsapi.Truncate) && h.flags.Access == fsapi.ReadOnly {
			return fsapi.Result{}, fsapi.Permission
		}
		if c.Operation == fsapi.Write && h.flags.Append {
			o, err := s.applied.Store.Stat(ctx, c.Object)
			if err != nil {
				return fsapi.Result{}, err
			}
			c.Offset = o.Attr.Size
		}
	}
	image, err := s.applied.Store.Export(ctx)
	if err != nil {
		return fsapi.Result{}, s.block(err)
	}
	p := newPlan(image, s.config.Clock.Now())
	if !p.now.Valid() {
		return fsapi.Result{}, fsapi.Invalid
	}
	if err = s.permissions(ctx, cred, original, c, p); err != nil {
		return fsapi.Result{}, err
	}
	result := fsapi.Result{Position: s.selection.Through.Position}
	switch c.Operation {
	case fsapi.Lookup:
		if _, err = p.directory(c.Parent); err != nil {
			return result, err
		}
		id := c.Parent
		switch c.Name {
		case ".":
		case "..":
			id = p.objects[c.Parent].Parent.Object
		default:
			if err = validName(c.Name); err != nil {
				return result, err
			}
			entry := p.entry(c.Parent, c.Name)
			if entry.Target == nil {
				return result, fsapi.NotFound
			}
			id = *entry.Target
		}
		if id.Home != c.Mount {
			return result, fsapi.Unsupported
		}
		o, err := p.object(id)
		if err != nil {
			return result, err
		}
		if err = s.acquire(ctx, id, true, nil); err != nil {
			return result, err
		}
		result.Attr = &o.Attr
	case fsapi.Getattr:
		o, err := p.object(c.Object)
		if err != nil {
			return result, err
		}
		result.Attr = &o.Attr
	case fsapi.Open:
		if err = c.Flags.Validate(); err != nil {
			return result, err
		}
		if c.Flags.Create || c.Flags.Exclusive {
			return result, fsapi.Invalid
		}
		o, err := p.object(c.Object)
		if err != nil {
			return result, err
		}
		if o.Attr.Kind == fsapi.Symlink {
			return result, fsapi.Unsupported
		}
		if o.Attr.Kind == fsapi.Directory && c.Flags.Access != fsapi.ReadOnly {
			return result, fsapi.IsDirectory
		}
		if len(s.handles) >= s.config.Limits.Handles {
			return result, fsapi.NoSpace
		}
		if c.Flags.Truncate {
			tc := c
			tc.Operation = fsapi.Truncate
			tc.Size = 0
			if _, err = s.mutate(ctx, cred, tc, p); err != nil {
				return result, err
			}
			o, err = s.applied.Store.Stat(ctx, c.Object)
			if err != nil {
				return result, err
			}
		}
		if err = s.acquire(ctx, c.Object, false, &c.Flags); err != nil {
			return result, err
		}
		result.Handle = s.nextHandle
		result.Attr = &o.Attr
	case fsapi.Read:
		if c.Handle == 0 {
			return result, fsapi.Stale
		}
		if c.Size > int64(s.config.Limits.IOBytes) || fsapi.ValidateRange(c.Offset, c.Size) != nil {
			return result, fsapi.Invalid
		}
		o, err := p.object(c.Object)
		if err != nil {
			return result, err
		}
		if o.Attr.Kind != fsapi.Regular {
			return result, fsapi.IsDirectory
		}
		data := make([]byte, int(c.Size))
		n, err := s.applied.Store.Read(ctx, c.Object, data, c.Offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return result, s.block(err)
		}
		result.Data = data[:n]
		result.Count = int64(n)
	case fsapi.Readlink:
		o, err := p.object(c.Object)
		if err != nil {
			return result, err
		}
		if o.Attr.Kind != fsapi.Symlink {
			return result, fsapi.Invalid
		}
		result.Data = slices.Clone(o.Text)
	case fsapi.Readdir:
		if c.After != "" && c.DirectoryRevision == 0 {
			return result, fsapi.Invalid
		}
		o, err := p.directory(c.Object)
		if err != nil {
			return result, err
		}
		if c.DirectoryRevision != 0 && c.DirectoryRevision != o.Attr.Revision {
			return result, fsapi.Stale
		}
		entries, err := s.applied.Store.ReadDir(ctx, c.Object, c.After, c.Limit)
		if err != nil {
			return result, err
		}
		result.DirectoryRevision = o.Attr.Revision
		for _, e := range entries {
			if e.Target.Home != c.Mount {
				return result, fsapi.Unsupported
			}
			target, err := p.object(*e.Target)
			if err != nil {
				return result, err
			}
			result.Entries = append(result.Entries, fsapi.DirEntry{Name: e.Name, Object: *e.Target, Revision: e.Revision, Kind: target.Attr.Kind})
		}
	case fsapi.Statfs:
		var used uint64
		for _, o := range image.Objects {
			used += uint64(o.Attr.Size)
		}
		maxObjects := s.config.Storage.Limits.Objects
		if maxObjects == 0 {
			maxObjects = 4096
		}
		totalObjects := len(image.Objects) + len(image.Retired)
		result.Capacity = &fsapi.Capacity{Objects: uint64(maxObjects), ObjectsFree: uint64(max(0, maxObjects-totalObjects)), Bytes: uint64(s.config.Limits.LogicalBytes), BytesFree: uint64(s.config.Limits.LogicalBytes) - used}
	case fsapi.Sync:
		if !c.Durability.Valid() {
			return result, fsapi.Invalid
		}
		o, err := p.object(c.Object)
		if err != nil {
			return result, err
		}
		if (c.Durability == fsapi.DirectoryDurability) != (o.Attr.Kind == fsapi.Directory) {
			return result, fsapi.Invalid
		}
		// Every earlier command is already protected and applied. A conservative
		// per-volume barrier includes them all, without fabricating BARRIER evidence.
		if err = s.writer.Flush(ctx); err != nil {
			return result, s.block(err)
		}
	case fsapi.TryLock, fsapi.WaitLock, fsapi.GetLock, fsapi.Unlock, fsapi.CancelLock, fsapi.DescriptorClose, fsapi.ProcessExit, fsapi.SessionExit:
		return s.lockCommand(c)
	case fsapi.Reap:
		if err = s.reap(ctx); err != nil {
			return result, err
		}
	case fsapi.Release:
		h, ok := s.handles[c.Handle]
		if !ok {
			return result, fsapi.Stale
		}
		if err = s.closeLocks(c, h.object); err != nil {
			return result, err
		}
		if err = s.drop(ctx, h.object, false, 1); err != nil {
			return result, err
		}
		delete(s.handles, c.Handle)
		if err = s.reap(ctx); err != nil {
			return result, err
		}
	case fsapi.Forget:
		if err = s.drop(ctx, c.Object, true, c.Count); err != nil {
			return result, err
		}
		if err = s.reap(ctx); err != nil {
			return result, err
		}
	default:
		return s.mutate(ctx, cred, c, p)
	}
	result.Position = s.selection.Through.Position
	return result, nil
}
func (s *Service) permissions(ctx context.Context, cred fsapi.Cred, original, resolved fsapi.Command, p *plan) error {
	if s.config.Permissions == nil {
		return nil
	} // Explicit validation profile only.
	input := PermissionContext{Command: original.Clone(), Object: resolved.Object, Handle: resolved.Handle != 0}
	if e := p.entry(original.Parent, original.Name); e.Target != nil {
		input.Source = *e.Target
	}
	if e := p.entry(original.Destination, original.NewName); e.Target != nil {
		input.Replacement = *e.Target
	}
	set := map[fsapi.ObjectID]bool{}
	for _, id := range []fsapi.ObjectID{input.Object, original.Parent, original.Destination, input.Source, input.Replacement} {
		if id.Valid() {
			set[id] = true
		}
	}
	attrs := make([]fsapi.Attr, 0, len(set))
	for id := range set {
		if o, ok := p.objects[id]; ok {
			attrs = append(attrs, o.Attr)
		}
	}
	slices.SortFunc(attrs, func(a, b fsapi.Attr) int { return compareID(a.Object, b.Object) })
	input.Attributes = slices.Clone(attrs)
	decision, err := s.config.Permissions.Check(ctx, cred.Clone(), input)
	if err != nil {
		return err
	}
	if !decision.View.Valid() || decision.Revision == 0 {
		return fsapi.Permission
	}
	groups := slices.Clone(cred.Groups)
	slices.Sort(groups)
	groups = slices.Compact(groups)
	if len(groups) > 1024 {
		return fsapi.Invalid
	}
	for _, a := range attrs {
		p.authorizations = append(p.authorizations, record.Authorization{Object: a.Object, ObjectRevision: a.Revision, View: decision.View, PolicyRevision: decision.Revision, Credentials: record.Credentials{UID: cred.UID, GID: cred.GID, Groups: slices.Clone(groups)}})
	}
	return nil
}
func (s *Service) acquire(ctx context.Context, id fsapi.ObjectID, lookup bool, flags *fsapi.OpenFlags) error {
	if lookup && s.lookupCount >= uint64(s.config.Limits.Lookups) || flags != nil && (len(s.handles) >= s.config.Limits.Handles || s.nextHandle == fsapi.OpenHandle(math.MaxUint64)) {
		return fsapi.NoSpace
	}
	life := s.lifetimes[id]
	if life == nil {
		op, err := s.newUUID()
		if err != nil {
			return err
		}
		pin, err := s.newUUID()
		if err != nil {
			return err
		}
		// A conservative pin covers lookup and open obligations together. Each pin
		// command gets its own operation UUID; it never aliases the creating COMMIT.
		payload := record.HandlePin{Pin: pin, Object: id, ServingGeneration: s.config.Workspace.ID, Incarnation: s.config.Authority.OwnerIncarnation, Reasons: 5}
		if err = s.protect(ctx, op, payload); err != nil {
			return err
		}
		life = &lifetime{pin: pin}
		s.lifetimes[id] = life
	}
	if lookup {
		life.lookups++
		s.lookupCount++
	}
	if flags != nil {
		s.nextHandle++
		s.handles[s.nextHandle] = handle{object: id, flags: *flags}
		life.handles++
	}
	return nil
}
func (s *Service) drop(ctx context.Context, id fsapi.ObjectID, lookup bool, count uint64) error {
	life := s.lifetimes[id]
	if life == nil || count == 0 {
		return fsapi.Stale
	}
	remainingLookups, remainingHandles := life.lookups, life.handles
	if lookup {
		if count > remainingLookups {
			return fsapi.Invalid
		}
		remainingLookups -= count
	} else {
		if count != 1 || remainingHandles == 0 {
			return fsapi.Stale
		}
		remainingHandles--
	}
	if remainingLookups == 0 && remainingHandles == 0 {
		op, err := s.newUUID()
		if err != nil {
			return err
		}
		if err = s.protect(ctx, op, record.HandleRelease{Pin: life.pin, Object: id, ServingGeneration: s.config.Workspace.ID, Incarnation: s.config.Authority.OwnerIncarnation, Reason: 1}); err != nil {
			return err
		}
		delete(s.lifetimes, id)
	} else {
		life.lookups = remainingLookups
		life.handles = remainingHandles
	}
	if lookup {
		s.lookupCount -= count
	}
	return nil
}
func validName(name string) error {
	if len(name) > 255 {
		return fsapi.NameTooLong
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return fsapi.Invalid
	}
	return nil
}
