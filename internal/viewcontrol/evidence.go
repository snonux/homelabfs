package viewcontrol

import (
	"bytes"
	"context"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
)

// LedgerEvidence is inert read-only evidence about the actual protected
// controller ledger. Digest is the hash SuspendRestart compares; Matches says
// whether the live in-memory state equals those protected bytes. Counts only
// describe that state. None of these values is a restart token, source receipt,
// physical acknowledgement or restoration authority.
type LedgerEvidence struct {
	Digest                                 string
	Bytes                                  int
	Matches                                bool
	Device, Inode                          uint64
	Sources, Leases, Reservations, Domains int
	Tickets, Terminal                      int
	Recall                                 bool
}

// LedgerEvidence captures the protected controller state under the controller
// mutex, so no ticket/recall transition is half observed. It allocates at most
// two copies bounded by the signed LedgerBytes (protected bytes plus one
// marshalled comparison copy); runtime callers fund them explicitly. A closed or
// poisoned controller truthfully reports Unavailable and is never reopened here.
func (c *Controller) LedgerEvidence(ctx context.Context) (LedgerEvidence, error) {
	if err := ctx.Err(); err != nil {
		return LedgerEvidence{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() || c.poisoned.Load() {
		return LedgerEvidence{}, fsapi.Unavailable
	}
	var protected []byte
	if err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(version))
		if b == nil {
			return fsapi.Stale
		}
		protected = bytes.Clone(b.Get([]byte("state")))
		return nil
	}); err != nil {
		return LedgerEvidence{}, err
	}
	live, err := c.rawState()
	if err != nil {
		return LedgerEvidence{}, err
	}
	s := c.state
	e := LedgerEvidence{Digest: hash(protected), Bytes: len(protected), Matches: bytes.Equal(live, protected), Sources: len(s.Sources), Leases: len(s.Leases), Reservations: len(s.Reservations), Domains: len(s.Domains), Tickets: len(s.Tickets), Terminal: len(s.Terminal), Recall: s.Recall != nil}
	if c.file != nil {
		if st, ok := c.file.Sys().(*syscall.Stat_t); ok {
			e.Device, e.Inode = uint64(st.Dev), uint64(st.Ino)
		}
	}
	return e, nil
}
