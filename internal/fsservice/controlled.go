package fsservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

// ControlledCut is sealed to the local service's serialized worker. Its methods
// are valid only during Controlled. It exposes no backing filesystem authority.
type ControlledCut struct {
	service  *Service
	active   bool
	admitted bool
	permit   ControlledPermit
}

// ControlledPlan is private validation recovery evidence, not a v1 journal kind
// or a normal publication certificate. Persist it before ApplyControlled.
type ControlledPlan struct {
	LookupPin        bool
	Operation        fsapi.UUID
	Command          fsapi.Command
	Credentials      fsapi.Cred
	Before           fsapi.Position
	Mutation         *record.Mutation `json:"-"`
	MutationEncoding []byte
	Result           fsapi.Result
	Object           fsapi.ObjectID
	TooLarge         bool
	Pin              *record.HandlePin
	PinOperation     fsapi.UUID
}

// ControlledJournal identifies the actual admitted local source and owner stream.
// It is a private enrollment fact, not normal publication or replication proof.
type ControlledJournal struct {
	Stream             segment.Stream
	Owner, Incarnation fsapi.UUID
	Source             vfs.Identity
}

// ControlledJournalInfo returns actual journal identity only for the named
// local disposable profile. The root descriptor is the service's admitted root.
func (s *Service) ControlledJournalInfo(ctx context.Context, profile ValidationProfile) (ControlledJournal, error) {
	if s.config.Validation == nil || *s.config.Validation != profile || !profile.Disposable {
		return ControlledJournal{}, fsapi.Permission
	}
	for _, root := range []storage.Filesystem{s.config.Source, s.config.Reservations, s.config.Workspace.Data, s.config.Workspace.Journal, s.config.Workspace.Progress} {
		if _, ok := root.(*vfs.OS); !ok {
			return ControlledJournal{}, fsapi.Permission
		}
	}
	info, err := s.config.Source.Stat(ctx, ".")
	if err != nil {
		return ControlledJournal{}, err
	}
	return ControlledJournal{Stream: s.config.Stream, Owner: s.config.Authority.Owner, Incarnation: s.config.Authority.OwnerIncarnation, Source: info.Identity}, nil
}

// Controlled serializes capture, durable private planning and execution with
// every native command. The named disposable profile and Unix policy must match.
// An admitted closure runs despite caller cancellation, just like Dispatch.
func (s *Service) Controlled(ctx context.Context, profile ValidationProfile, run func(context.Context, *ControlledCut) error) error {
	if run == nil || s.config.Validation == nil || profile != *s.config.Validation || !profile.Disposable || s.config.Permissions == nil || s.config.Reclamation != nil {
		return fsapi.Permission
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	s.mu.Lock()
	if s.closed || s.State() != Serving {
		s.mu.Unlock()
		return fsapi.Unavailable
	}
	task := command{controlled: func(owned context.Context) {
		if s.State() != Serving {
			done <- fsapi.Unavailable
			return
		}
		cut := &ControlledCut{service: s, active: true}
		defer func() { cut.active = false }()
		done <- run(owned, cut)
	}}
	select {
	case s.queue <- task:
		s.mu.Unlock()
	default:
		s.mu.Unlock()
		return fsapi.NoSpace
	}
	return <-done
}

// Image snapshots the actual committed applied metadata on the worker.
func (c *ControlledCut) Image(ctx context.Context, volume fsapi.UUID) (storage.Image, error) {
	if !c.accessAllowed() || volume != c.service.config.Storage.Volume {
		return storage.Image{}, fsapi.Permission
	}
	return c.service.applied.Store.Export(ctx)
}

// Stat resolves a stable logical object without pathname traversal.
func (c *ControlledCut) Stat(ctx context.Context, id fsapi.ObjectID) (storage.Object, error) {
	if !c.accessAllowed() {
		return storage.Object{}, fsapi.Stale
	}
	return c.service.applied.Store.Stat(ctx, id)
}

// Lookup reads an entry at this same serialized cut.
func (c *ControlledCut) Lookup(ctx context.Context, id fsapi.ObjectID, name string) (storage.Entry, error) {
	image, err := c.Image(ctx, id.Home)
	if err != nil {
		return storage.Entry{}, err
	}
	p := newPlan(image, c.service.config.Clock.Now())
	if _, err = p.directory(id); err != nil {
		return storage.Entry{}, err
	}
	return p.entry(id, name), nil
}

// PlanControlled invokes the shared Unix planner and captures exact afterimages
// and semantic output before any filesystem effect. maxEnd is used only for
// delegated WRITE and after resolving the authoritative append offset.
func (c *ControlledCut) PlanControlled(ctx context.Context, cred fsapi.Cred, command fsapi.Command, operation fsapi.UUID, allocation fsapi.ObjectID, maxEnd *int64) (ControlledPlan, error) {
	return c.planControlled(ctx, cred, command, operation, allocation, maxEnd, false)
}

// PlanControlledDrain preserves the verified mmap positional offset even for an
// append description. Only an immutable-drain-validated admission can invoke it.
func (c *ControlledCut) PlanControlledDrain(ctx context.Context, cred fsapi.Cred, command fsapi.Command, operation fsapi.UUID, allocation fsapi.ObjectID, maxEnd *int64) (ControlledPlan, error) {
	verifier, ok := c.permit.(interface {
		CheckDrain(context.Context, fsapi.Command) error
	})
	if !ok {
		return ControlledPlan{}, fsapi.Permission
	}
	if err := verifier.CheckDrain(ctx, command); err != nil {
		return ControlledPlan{}, err
	}
	return c.planControlled(ctx, cred, command, operation, allocation, maxEnd, true)
}
func (c *ControlledCut) planControlled(ctx context.Context, cred fsapi.Cred, command fsapi.Command, operation fsapi.UUID, allocation fsapi.ObjectID, maxEnd *int64, positionalDrain bool) (ControlledPlan, error) {
	if !c.accessAllowed() || !operation.Valid() {
		return ControlledPlan{}, fsapi.Invalid
	}
	s := c.service
	if s.ids[operation] {
		return ControlledPlan{}, fsapi.Stale
	}
	if len(s.ids) >= s.config.Apply.MaxFrames+s.config.Limits.Reservations*2+s.config.Limits.Handles*2 {
		return ControlledPlan{}, fsapi.NoSpace
	}
	if allocation.Valid() {
		if allocation.Home != s.config.Storage.Volume || s.allocations[allocation] {
			return ControlledPlan{}, fsapi.Stale
		}
		if len(s.allocations) >= s.config.Limits.Reservations {
			return ControlledPlan{}, fsapi.NoSpace
		}
		s.allocations[allocation] = true
	}
	s.ids[operation] = true
	cp := ControlledPlan{Operation: operation, Command: command.Clone(), Credentials: cred.Clone(), Before: s.selection.Through.Position, LookupPin: command.Operation == fsapi.Create || command.Operation == fsapi.Mkdir || command.Operation == fsapi.SymlinkOp || command.Operation == fsapi.Lookup}
	command = command.Clone()
	if len(command.Data) > s.config.Limits.IOBytes || len(command.Name) > 255 || len(command.NewName) > 255 || command.Size < 0 {
		return cp, fsapi.Invalid
	}
	if (command.Operation == fsapi.RenameOp || command.Operation == fsapi.LinkOp) && command.DestinationMount != command.Mount {
		return cp, fsapi.CrossDevice
	}
	for _, object := range []fsapi.ObjectID{command.Object, command.Parent, command.Destination} {
		if object.Valid() && object.Home != command.Mount {
			return cp, fsapi.CrossDevice
		}
	}
	if command.Mount != s.config.Storage.Volume {
		return cp, fsapi.CrossDevice
	}
	if command.Generation.Valid() && command.Generation != s.config.Workspace.ID {
		return cp, fsapi.Stale
	}
	if command.Handle != 0 {
		h, ok := s.handles[command.Handle]
		if !ok || h.object != command.Object {
			return cp, fsapi.Stale
		}
		if command.Operation == fsapi.Read && h.flags.Access == fsapi.WriteOnly || (command.Operation == fsapi.Write || command.Operation == fsapi.Truncate) && h.flags.Access == fsapi.ReadOnly {
			return cp, fsapi.Permission
		}
		if command.Operation == fsapi.Write && h.flags.Append && !positionalDrain {
			o, e := s.applied.Store.Stat(ctx, h.object)
			if e != nil {
				return cp, e
			}
			command.Offset = o.Attr.Size
		}
	}
	image, err := s.applied.Store.Export(ctx)
	if err != nil {
		return cp, err
	}
	p := newPlan(image, s.config.Clock.Now())
	if err = s.permissions(ctx, cred, cp.Command, command, p); err != nil {
		return cp, err
	}
	if maxEnd != nil {
		if command.Operation != fsapi.Write || *maxEnd < 0 || command.Offset < 0 {
			return cp, fsapi.Invalid
		}
		if command.Offset >= *maxEnd {
			cp.TooLarge = true
			return cp, nil
		}
		if int64(len(command.Data)) > *maxEnd-command.Offset {
			command.Data = slices.Clone(command.Data[:*maxEnd-command.Offset])
		}
	}
	cp.Command = command
	if observed, handled, e := observeCommitted(ctx, command, p, image, s.applied.Store, s.config.Limits, s.config.Storage.Limits); handled {
		cp.Result = observed
		cp.Result.Generation = s.config.Workspace.ID
		return cp, e
	}
	if command.Operation == fsapi.Create && !command.Flags.Exclusive {
		if entry := p.entry(command.Parent, command.Name); entry.Target != nil {
			command.Operation = fsapi.Open
			command.Object = *entry.Target
			command.Flags.Create = false
			cp.Command = command
		}
	}
	switch command.Operation {
	case fsapi.Create, fsapi.Mkdir, fsapi.SymlinkOp, fsapi.Unlink, fsapi.Rmdir, fsapi.RenameOp, fsapi.LinkOp, fsapi.Write, fsapi.Truncate, fsapi.Setattr:
		if (command.Operation == fsapi.Create || command.Operation == fsapi.Mkdir || command.Operation == fsapi.SymlinkOp) && s.lookupCount >= uint64(s.config.Limits.Lookups) {
			return cp, fsapi.NoSpace
		}
		if command.Operation == fsapi.Create && len(s.handles) >= s.config.Limits.Handles {
			return cp, fsapi.NoSpace
		}
		config := planningConfig{root: image.Root, storageLimits: s.config.Storage.Limits, logicalBytes: s.config.Limits.LogicalBytes, identities: func(_ context.Context, creation bool) (fsapi.UUID, fsapi.ObjectID, error) {
			if creation && (!allocation.Valid() || allocation.Home != command.Mount) {
				return fsapi.UUID{}, fsapi.ObjectID{}, fsapi.Invalid
			}
			return operation, allocation, nil
		}}
		prepared, e := prepareMutation(ctx, cred, command, p, config)
		if e != nil {
			return cp, e
		}
		cp.Mutation = prepared.Mutation
		if cp.Mutation != nil {
			header := s.header(operation)
			header.Sequence = 1
			cp.MutationEncoding, err = record.Encode(record.Record{Header: header, Payload: *cp.Mutation})
			if err != nil {
				return cp, err
			}
		}
		cp.Object = prepared.Object
		cp.Result = prepared.Result
		if cp.Object.Valid() {
			o, e := p.object(cp.Object)
			if e != nil {
				return cp, e
			}
			attr := o.Attr
			cp.Result.Attr = &attr
		}
		if command.Operation == fsapi.Create {
			cp.Result.Handle = s.nextHandle + 1
		}
	case fsapi.Open:
		if err = command.Flags.Validate(); err != nil {
			return cp, err
		}
		if command.Flags.Create || command.Flags.Exclusive {
			return cp, fsapi.Unsupported
		}
		o, e := p.object(command.Object)
		if e != nil {
			return cp, e
		}
		if o.Attr.Kind == fsapi.Symlink {
			return cp, fsapi.Unsupported
		}
		if o.Attr.Kind == fsapi.Directory && command.Flags.Access != fsapi.ReadOnly {
			return cp, fsapi.IsDirectory
		}
		if len(s.handles) >= s.config.Limits.Handles {
			return cp, fsapi.NoSpace
		}
		if command.Flags.Truncate {
			if o.Attr.Kind != fsapi.Regular {
				return cp, fsapi.Invalid
			}
			truncate := command.Clone()
			truncate.Operation = fsapi.Truncate
			truncate.Size = 0
			config := planningConfig{root: image.Root, storageLimits: s.config.Storage.Limits, logicalBytes: s.config.Limits.LogicalBytes, identities: func(context.Context, bool) (fsapi.UUID, fsapi.ObjectID, error) {
				return operation, fsapi.ObjectID{}, nil
			}}
			prepared, e := prepareMutation(ctx, cred, truncate, p, config)
			if e != nil {
				return cp, e
			}
			cp.Mutation = prepared.Mutation
			o, e = p.object(command.Object)
			if e != nil {
				return cp, e
			}
		}
		cp.Object = command.Object
		cp.Result.Attr = &o.Attr
		cp.Result.Handle = s.nextHandle + 1
	case fsapi.Lookup:
		if _, err = p.directory(command.Parent); err != nil {
			return cp, err
		}
		entry := p.entry(command.Parent, command.Name)
		if entry.Target == nil {
			return cp, fsapi.NotFound
		}
		o, e := p.object(*entry.Target)
		if e != nil {
			return cp, e
		}
		cp.Object = *entry.Target
		cp.Result.Attr = &o.Attr
	case fsapi.Sync:
		if !command.Durability.Valid() {
			return cp, fsapi.Invalid
		}
		object, err := p.object(command.Object)
		if err != nil {
			return cp, err
		}
		if (command.Durability == fsapi.DirectoryDurability) != (object.Attr.Kind == fsapi.Directory) {
			return cp, fsapi.Invalid
		}
	case fsapi.Release:
		life := s.lifetimes[command.Object]
		if life == nil || life.handles == 0 || !command.DescriptionOnly {
			return cp, fsapi.Stale
		}
	case fsapi.Forget:
		life := s.lifetimes[command.Object]
		if life == nil || command.Count == 0 || command.Count > life.lookups {
			return cp, fsapi.Stale
		}
	case fsapi.DescriptorClose, fsapi.ProcessExit:
		owner := command.Lock.Owner
		if !owner.Valid() || owner.Generation != s.config.Workspace.ID {
			return cp, fsapi.Stale
		}
		if command.Operation == fsapi.DescriptorClose && command.Handle == 0 {
			return cp, fsapi.Stale
		}
		if command.Operation == fsapi.ProcessExit {
			if _, ok := s.owners[owner]; !ok && len(s.owners) >= s.config.Limits.Locks {
				return cp, fsapi.NoSpace
			}
		}
	default:
		return cp, fsapi.Unsupported
	}
	cp.Result.Generation = s.config.Workspace.ID
	if cp.Mutation != nil && len(cp.MutationEncoding) == 0 {
		header := s.header(operation)
		header.Sequence = 1
		cp.MutationEncoding, err = record.Encode(record.Record{Header: header, Payload: *cp.Mutation})
		if err != nil {
			return cp, err
		}
	}
	if cp.Object.Valid() && (command.Operation == fsapi.Create || command.Operation == fsapi.Mkdir || command.Operation == fsapi.SymlinkOp || command.Operation == fsapi.Lookup || command.Operation == fsapi.Open) && s.lifetimes[cp.Object] == nil {
		op, err := s.newUUID()
		if err != nil {
			return cp, err
		}
		pin, err := s.newUUID()
		if err != nil {
			return cp, err
		}
		if cp.Mutation == nil {
			op = operation
		}
		cp.PinOperation = op
		cp.Pin = &record.HandlePin{Pin: pin, Object: cp.Object, ServingGeneration: s.config.Workspace.ID, Incarnation: s.config.Authority.OwnerIncarnation, Reasons: 5}
	}
	if err = c.checkSourcePlan(ctx, cp); err != nil {
		return ControlledPlan{}, err
	}
	return cp, nil
}

// ApplyControlled applies the exact protected plan. Recovery finds original
// operation UUID evidence and compares the complete mutation; it never replans
// against a pathname. Incomplete original journal evidence keeps the gate shut.
func (c *ControlledCut) ApplyControlled(ctx context.Context, p ControlledPlan, recovery bool) (fsapi.Result, error) {
	if !c.accessAllowed() {
		return fsapi.Result{}, fsapi.Stale
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return fsapi.Result{}, err
	}
	binding := sha256.Sum256(raw)
	if recovery && controlledLifetime(p.Command.Operation) && p.Result.Code == fsapi.OK && !p.TooLarge {
		if p.Result.Generation == c.service.config.Workspace.ID {
			evidence, ok := c.service.controlledEffects[p.Operation]
			if !ok || evidence != binding {
				return fsapi.Result{}, fsapi.Unavailable
			}
			return p.Result, nil
		}
		if p.Mutation == nil {
			return fsapi.Result{}, fsapi.Unavailable
		}
	}
	result, err := c.applyControlled(ctx, p, recovery)
	if err == nil && controlledLifetime(p.Command.Operation) {
		if c.service.controlledEffects == nil {
			c.service.controlledEffects = make(map[fsapi.UUID][32]byte)
		}
		c.service.controlledEffects[p.Operation] = binding
	}
	return result, err
}
func controlledLifetime(op fsapi.Operation) bool {
	switch op {
	case fsapi.Create, fsapi.Mkdir, fsapi.SymlinkOp, fsapi.Open, fsapi.Lookup, fsapi.Release, fsapi.Forget, fsapi.DescriptorClose, fsapi.ProcessExit:
		return true
	}
	return false
}
func (c *ControlledCut) applyControlled(ctx context.Context, p ControlledPlan, recovery bool) (fsapi.Result, error) {
	if !c.accessAllowed() {
		return fsapi.Result{}, fsapi.Stale
	}
	s := c.service
	if p.TooLarge || p.Result.Code != fsapi.OK {
		return p.Result, nil
	}
	if recovery {
		if p.Command.Operation == fsapi.Sync {
			if err := s.writer.Flush(ctx); err != nil {
				return fsapi.Result{}, s.block(err)
			}
		}
		found, err := s.controlledEvidence(ctx, p)
		if err != nil {
			return fsapi.Result{}, err
		}
		if p.Mutation != nil && !found {
			return fsapi.Result{}, fsapi.Unavailable
		}
		if p.Pin != nil {
			pinned, err := s.controlledPinEvidence(ctx, p)
			if err != nil {
				return fsapi.Result{}, err
			}
			if !pinned {
				if err = s.protect(ctx, p.PinOperation, *p.Pin); err != nil {
					return fsapi.Result{}, err
				}
			}
		}
		return p.Result, nil
	}
	if p.Before != s.selection.Through.Position {
		return fsapi.Result{}, fsapi.Stale
	}
	if p.Mutation != nil {
		if err := s.protect(ctx, p.Operation, *p.Mutation); err != nil {
			return fsapi.Result{}, err
		}
	}
	command := p.Command
	switch command.Operation {
	case fsapi.Create, fsapi.Mkdir, fsapi.SymlinkOp, fsapi.Lookup, fsapi.Open:
		var flags *fsapi.OpenFlags
		lookup := p.LookupPin
		if command.Operation == fsapi.Create || command.Operation == fsapi.Open {
			flags = &command.Flags
		}
		if p.Pin != nil {
			if err := s.protect(ctx, p.PinOperation, *p.Pin); err != nil {
				return fsapi.Result{}, err
			}
			s.lifetimes[p.Object] = &lifetime{pin: p.Pin.Pin}
		}
		if err := s.acquire(ctx, p.Object, lookup, flags); err != nil {
			return fsapi.Result{}, err
		}
		if flags != nil && s.nextHandle != p.Result.Handle {
			return fsapi.Result{}, s.block(fsapi.Stale)
		}
	case fsapi.Sync, fsapi.Release, fsapi.Forget, fsapi.DescriptorClose, fsapi.ProcessExit:
		result, err := s.execute(ctx, p.Credentials, command)
		if err != nil {
			return fsapi.Result{}, err
		}
		return result, nil
	}
	return p.Result, nil
}
func (s *Service) controlledEvidence(ctx context.Context, p ControlledPlan) (bool, error) {
	reader, err := s.reader(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = reader.Close() }()
	var mutation *record.FrameRef
	for {
		e, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if e.Record.Header.Operation != p.Operation {
			continue
		}
		switch value := e.Record.Payload.(type) {
		case record.Mutation:
			a, err := json.Marshal(value)
			if err != nil {
				return false, err
			}
			b, err := json.Marshal(p.Mutation)
			if err != nil {
				return false, err
			}
			if !bytes.Equal(a, b) {
				return false, fsapi.Stale
			}
			ref := e.Reference
			mutation = &ref
		case record.Commit:
			if mutation == nil || value.Mutation != *mutation {
				return false, fsapi.Stale
			}
			return true, nil
		}
	}
}

func (s *Service) controlledPinEvidence(ctx context.Context, p ControlledPlan) (bool, error) {
	reader, err := s.reader(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = reader.Close() }()
	for {
		e, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if e.Record.Header.Operation != p.PinOperation {
			continue
		}
		pin, ok := e.Record.Payload.(record.HandlePin)
		if !ok || p.Pin == nil || pin != *p.Pin {
			return false, fsapi.Stale
		}
		return true, nil
	}
}
