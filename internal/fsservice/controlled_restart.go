package fsservice

import (
	"context"
	"github.com/snonux/homelabfs/internal/storage"
	"io"
	"path"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

// ControlledRestart retains exact already admitted backing descriptors and
// construction policy across a barriered component restart. It neither restores
// ephemeral native description/lookup/lock tables nor grants FUSE authority.
type ControlledRestart struct {
	mu        sync.Mutex
	config    Config
	roots     []vfs.Identity
	used      bool
	inventory []controlledRootEntry
}

// SuspendControlled closes the service after the runtime joined its old bridge
// workers. It preserves every journal, catalog, payload and reservation root.
func (s *Service) SuspendControlled(ctx context.Context, maxBytes int64) (*ControlledRestart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.config.Validation == nil || !s.config.Validation.Disposable {
		return nil, fsapi.Permission
	}
	identities, err := controlledRoots(ctx, s.config)
	if err != nil {
		return nil, err
	}
	var applied *apply.BarrieredRestart
	err = s.Controlled(ctx, *s.config.Validation, func(ctx context.Context, cut *ControlledCut) error {
		if err := s.writer.Flush(ctx); err != nil {
			return err
		}
		reader, err := s.reader(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		applied, err = s.applied.SuspendRestart(ctx, reader, maxBytes)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err = s.Close(); err != nil {
		return nil, err
	}
	// Joining the service worker makes these exact native lifetime tables
	// stable. They cannot be guessed empty from bridge grant state.
	if len(s.handles) != 0 || s.lookupCount != 0 || len(s.locks) != 0 || len(s.pending) != 0 || len(s.waits) != 0 || len(s.ids) > 65536 {
		return nil, fsapi.Unavailable
	}
	c := s.config
	c.restartIDs = s.ids
	c.restartHandle = s.nextHandle
	c.restartApplied = applied
	// Move the bounded issuance map (at most8MiB conservative map ownership),
	// and drop the closed applied/source graph before reconstruction.
	s.ids = nil
	s.applied = apply.Result{}
	s.lifetimes = nil
	s.handles = nil
	s.pending = nil
	s.waits = nil
	inventory, err := controlledInventory(ctx, c)
	if err != nil {
		return nil, err
	}
	return &ControlledRestart{config: c, roots: identities, inventory: inventory}, nil
}

// Reopen rereads the real journal and applied state through the original owned
// descriptors. The original serving generation is retained only for supported
// scopes with no unreconstructed ephemeral native lifetime owners.
func (r *ControlledRestart) Reopen(ctx context.Context) (*Service, error) {
	if r == nil {
		return nil, fsapi.Permission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used {
		return nil, fsapi.Permission
	}
	r.used = true
	roots, err := controlledRoots(ctx, r.config)
	if err != nil {
		return nil, err
	}
	if len(roots) != len(r.roots) {
		return nil, fsapi.Stale
	}
	for i, identity := range roots {
		if identity != r.roots[i] {
			return nil, fsapi.Stale
		}
	}
	inventory, err := controlledInventory(ctx, r.config)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(inventory, r.inventory) {
		return nil, fsapi.Stale
	}
	service, err := New(ctx, r.config)
	if err != nil {
		return nil, err
	}
	r.inventory = nil
	r.config.restartIDs = nil
	r.config.restartApplied = nil
	return service, nil
}

func controlledRoots(ctx context.Context, c Config) ([]vfs.Identity, error) {
	roots := []interface {
		Stat(context.Context, string) (vfs.Info, error)
	}{c.Source, c.Reservations, c.Workspace.Data, c.Workspace.Journal, c.Workspace.Progress}
	result := make([]vfs.Identity, 0, len(roots))
	for _, root := range roots {
		info, err := root.Stat(ctx, ".")
		if err != nil {
			return nil, err
		}
		result = append(result, info.Identity)
	}
	return result, nil
}

// This finite inventory is continuity evidence for the actual retained root
// descriptors. It is never imported or accepted from a caller/serialized file.
type controlledRootEntry struct {
	Root int
	Path string
	Info vfs.Info
}

func controlledInventory(ctx context.Context, c Config) ([]controlledRootEntry, error) {
	roots := []storage.Filesystem{c.Source, c.Reservations, c.Workspace.Data, c.Workspace.Journal, c.Workspace.Progress}
	entries := make([]controlledRootEntry, 0, 64)
	for i, root := range roots {
		pending := []string{"."}
		for len(pending) > 0 {
			name := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if len(name) > 512 || len(entries) >= 65536 {
				return nil, fsapi.NoSpace
			}
			info, err := root.Stat(ctx, name)
			if err != nil {
				return nil, err
			}
			entries = append(entries, controlledRootEntry{Root: i, Path: name, Info: info})
			if !info.Mode.IsDir() {
				continue
			}
			directory, err := root.OpenDir(ctx, name)
			if err != nil {
				return nil, err
			}
			for {
				page, err := directory.ReadDir(ctx, 64)
				for _, entry := range page {
					if len(pending)+len(entries) >= 65536 {
						_ = directory.Close()
						return nil, fsapi.NoSpace
					}
					pending = append(pending, path.Join(name, entry.Name))
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					_ = directory.Close()
					return nil, err
				}
			}
			if err = directory.Close(); err != nil {
				return nil, err
			}
		}
	}
	slices.SortFunc(entries, func(a, b controlledRootEntry) int {
		if a.Root != b.Root {
			return a.Root - b.Root
		}
		return strings.Compare(a.Path, b.Path)
	})
	return entries, nil
}
