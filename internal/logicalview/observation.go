package logicalview

import (
	"context"
	"io"
	"math"
	"slices"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

// Close ends the observation exactly once.
func (o *Observation) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.store.mu.Lock()
	defer o.store.mu.Unlock()
	o.closed = true
	o.store.readers--
	o.version = nil
	return nil
}

// Cut returns the exact captured visible selected certificate, nil at baseline.
func (o *Observation) Cut() (*record.SelectedRef, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, fsapi.Stale
	}
	return cloneCut(o.version.cut), nil
}

// Image returns independently owned metadata from this same immutable cut.
// Response/planner buffers belong to the caller's bounded request reservation.
func (o *Observation) Image(ctx context.Context, volume fsapi.UUID) (storage.Image, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.check(ctx); err != nil {
		return storage.Image{}, err
	}
	s, ok := o.version.images[volume]
	if !ok {
		return storage.Image{}, fsapi.CrossDevice
	}
	return storage.CopyImage(s.Image), nil
}

// Stat returns the exact object's home metadata, including an unlinked object.
func (o *Observation) Stat(ctx context.Context, id fsapi.ObjectID) (storage.Object, error) {
	image, err := o.Image(ctx, id.Home)
	if err != nil {
		return storage.Object{}, err
	}
	for _, object := range image.Objects {
		if object.Attr.Object == id {
			return object, nil
		}
	}
	return storage.Object{}, fsapi.NotFound
}

// Lookup resolves the captured namespace and retains the stable foreign home.
func (o *Observation) Lookup(ctx context.Context, parent fsapi.ObjectID, name string) (storage.Entry, error) {
	image, err := o.Image(ctx, parent.Home)
	if err != nil {
		return storage.Entry{}, err
	}
	found := false
	for _, object := range image.Objects {
		if object.Attr.Object == parent {
			if object.Attr.Kind != fsapi.Directory {
				return storage.Entry{}, fsapi.NotDirectory
			}
			if object.Attr.Nlink == 0 {
				return storage.Entry{}, fsapi.NotFound
			}
			found = true
		}
	}
	if !found {
		return storage.Entry{}, fsapi.NotFound
	}
	for _, e := range image.Entries {
		if e.Parent == parent && e.Name == name && e.Target != nil {
			return e, nil
		}
	}
	return storage.Entry{}, fsapi.NotFound
}

// Read reads only owned logical RAM, including zero-filled holes and truncation.
func (o *Observation) Read(ctx context.Context, id fsapi.ObjectID, p []byte, offset int64) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.check(ctx); err != nil {
		return 0, err
	}
	if fsapi.ValidateRange(offset, int64(len(p))) != nil {
		return 0, fsapi.Invalid
	}
	image, ok := o.version.images[id.Home]
	if !ok {
		return 0, fsapi.CrossDevice
	}
	for i, payload := range image.Image.Payloads {
		if payload.Object != id {
			continue
		}
		data := image.Data[i]
		if len(p) == 0 {
			return 0, nil
		}
		if offset >= int64(len(data)) {
			return 0, io.EOF
		}
		n := copy(p, data[offset:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
	for _, object := range image.Image.Objects {
		if object.Attr.Object == id {
			return 0, fsapi.Invalid
		}
	}
	return 0, fsapi.NotFound
}

// ReadDir returns a bounded raw-name page from the captured namespace.
func (o *Observation) ReadDir(ctx context.Context, parent fsapi.ObjectID, after string, limit int) ([]storage.Entry, error) {
	if limit < 1 || limit > 1024 {
		return nil, fsapi.Invalid
	}
	object, err := o.Stat(ctx, parent)
	if err != nil {
		return nil, err
	}
	if object.Attr.Kind != fsapi.Directory {
		return nil, fsapi.NotDirectory
	}
	if object.Attr.Nlink == 0 {
		return nil, fsapi.NotFound
	}
	image, err := o.Image(ctx, parent.Home)
	if err != nil {
		return nil, err
	}
	var out []storage.Entry
	for _, e := range image.Entries {
		if e.Parent == parent && e.Name > after && e.Target != nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out[:min(len(out), limit)], nil
}

// Open retains an already authorized description grant against the captured
// object. The filesystem-service policy must authorize it before this call.
func (o *Observation) Open(ctx context.Context, id fsapi.ObjectID, flags fsapi.OpenFlags) (fsapi.OpenHandle, error) {
	object, err := o.Stat(ctx, id)
	if err != nil {
		return 0, err
	}
	if err = flags.Validate(); err != nil {
		return 0, err
	}
	if flags.Create || flags.Truncate || flags.Exclusive || object.Attr.Kind == fsapi.Symlink {
		return 0, fsapi.Invalid
	}
	if object.Attr.Kind == fsapi.Directory && flags.Access != fsapi.ReadOnly {
		return 0, fsapi.IsDirectory
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.check(ctx); err != nil {
		return 0, err
	}
	s := o.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.nextHandle == fsapi.OpenHandle(math.MaxUint64) || len(s.handles) >= s.config.Limits.Handles {
		return 0, fsapi.NoSpace
	}
	if s.pending != nil {
		for _, op := range s.pending.claim.Stage.Operations {
			if op.Kind == storage.Reclaim && op.Object == id {
				return 0, fsapi.Busy
			}
		}
	}
	next := s.nextHandle + 1
	if err = s.verifyHandle(ctx, HandleAdmission, next, openHandle{Object: id, Flags: flags}); err != nil {
		return 0, err
	}
	s.nextHandle = next
	s.handles[s.nextHandle] = openHandle{Object: id, Flags: flags}
	return s.nextHandle, nil
}

// ResolveHandle validates a serving-generation-bound description and its saved
// access grant. Chmod and unlink never substitute another home/object.
func (s *Store) ResolveHandle(generation fsapi.UUID, h fsapi.OpenHandle) (fsapi.ObjectID, fsapi.OpenFlags, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.handles[h]
	if s.closed || generation != s.config.Generation || !ok {
		return fsapi.ObjectID{}, fsapi.OpenFlags{}, fsapi.Stale
	}
	return v.Object, v.Flags, nil
}

// ReleaseHandle ends one exact description. Numeric IDs are never reused.
func (s *Store) ReleaseHandle(ctx context.Context, generation fsapi.UUID, h fsapi.OpenHandle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || generation != s.config.Generation {
		return fsapi.Stale
	}
	value, ok := s.handles[h]
	if !ok {
		return fsapi.Stale
	}
	if err := s.verifyHandle(ctx, HandleRetirement, h, value); err != nil {
		return err
	}
	delete(s.handles, h)
	return nil
}

// Generation returns this service lifetime's handle generation.
func (s *Store) Generation() fsapi.UUID { return s.config.Generation }

// Release drops this internal version pin once.
func (p *VersionPin) Release() {
	p.once.Do(func() {
		p.store.mu.Lock()
		defer p.store.mu.Unlock()
		p.version.pins--
		p.store.pinCount--
		p.version = nil
	})
}

func (o *Observation) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.closed {
		return fsapi.Stale
	}
	return nil
}

// Domains returns the immutable admitted-domain inventory, not cache evidence.
func (s *Store) Domains() []Domain {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.config.Domains)
}

// Volumes returns the complete captured closure member set.
func (o *Observation) Volumes(ctx context.Context) ([]fsapi.UUID, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.check(ctx); err != nil {
		return nil, err
	}
	out := make([]fsapi.UUID, 0, len(o.version.images))
	for id := range o.version.images {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b fsapi.UUID) int { return slices.Compare(a[:], b[:]) })
	return out, nil
}

// BelongsTo binds a planning session to the actual store, not a reused UUID.
func (o *Observation) BelongsTo(s *Store) bool { return o.store == s }

func (s *Store) verifyHandle(ctx context.Context, phase Phase, id fsapi.OpenHandle, h openHandle) error {
	a, err := ownedAuthority(s.config.Authority)
	if err != nil {
		return err
	}
	return s.config.Verifier.Check(ctx, Check{Phase: phase, Authority: a, Handle: &HandleCheck{ID: id, Object: h.Object, Flags: h.Flags, Generation: s.config.Generation}, Domains: slices.Clone(s.config.Domains)})
}

// PlanningLimit bounds immutable input copies before the service planner clones
// a command. Request IO must fit the claim and full-snapshot transient package.
func (s *Store) PlanningLimit() int64 {
	return min(s.config.Limits.MaxSnapshotBytes, s.config.Limits.MaxClaimBytes/4)
}
