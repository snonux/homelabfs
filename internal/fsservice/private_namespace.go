package fsservice

import (
	"context"
	"errors"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// CheckPrivateControlNamespace is read-only constructor preflight for synthetic
// control inodes. A preexisting workload root entry is a conflict, never hidden
// or overwritten. validName reserves the name on all subsequent ordinary paths.
// This administrative inventory check grants no workload credentials or serving
// authority and must finish before native publication begins.
func (s *Service) CheckPrivateControlNamespace(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.State() != Serving {
		return fsapi.Unavailable
	}
	if s.frontend || len(s.accepted) != 0 {
		return fsapi.Busy
	}
	_, err := s.applied.Store.Lookup(ctx, s.config.Storage.Root, ".homelabfs-control-v1")
	if errors.Is(err, fsapi.NotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fsapi.Exists
}
