package logicalview

import (
	"context"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// ReserveIdentity permanently reserves one immutable operation identity and,
// for creation, its chosen home allocation in the logical ledger. It performs
// no backing disk I/O. Recovery must preserve these reservations, even when no
// grant was selected. Exhaustion gates planning before publishing any effect.
func (s *Store) ReserveIdentity(ctx context.Context, claim fsapi.UUID, allocation fsapi.ObjectID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || !claim.Valid() {
		return fsapi.Invalid
	}
	if s.identities[claim] != 0 {
		return fsapi.Stale
	}
	if len(s.identities) >= s.config.Limits.Identities || len(s.used) >= s.config.Limits.Identities {
		return fsapi.NoSpace
	}
	if allocation != (fsapi.ObjectID{}) {
		if !allocation.Valid() {
			return fsapi.Invalid
		}
		if _, ok := s.visible.images[allocation.Home]; !ok {
			return fsapi.CrossDevice
		}
		if s.used[allocation] {
			return fsapi.Stale
		}
		s.used[allocation] = true
		s.allocations[allocation] = claim
	}
	s.identities[claim] = 1
	return nil
}
