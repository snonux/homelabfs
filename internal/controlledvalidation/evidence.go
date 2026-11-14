package controlledvalidation

import (
	"context"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
)

// LedgerEvidence is an inert, read-only description of the actual protected
// provider ledger at one instant. It names the same complete key/value/sequence
// digest SuspendRestart compares, plus the exact ledger inode. It is not a
// restart token, restoration authority or claim/result acceptance. Claims
// (b84) summarizes the claim states read in the same transaction as Digest
// and Bytes; see evidence_claims.go.
type LedgerEvidence struct {
	Ledger string // enrolled ledger UUID, textual
	Digest string
	Device uint64
	Inode  uint64
	Bytes  int64
	Claims ClaimEvidence
}

// LedgerEvidence captures the current protected ledger digest under the same
// execution mutex as Execute/SuspendRestart, so it never observes a half-applied
// provider transition. It holds no lock afterwards and changes no state. A
// closed (suspended or released) provider truthfully reports Unavailable.
func (p *Provider) LedgerEvidence(ctx context.Context) (LedgerEvidence, error) {
	if err := ctx.Err(); err != nil {
		return LedgerEvidence{}, err
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	if p.closed.Load() || p.file == nil {
		return LedgerEvidence{}, fsapi.Unavailable
	}
	e := LedgerEvidence{Ledger: p.config.Authorization.Enrollment.Ledger.String()}
	// One read transaction gives the digest, the size and the claim summary
	// (b84) of the same snapshot. The bbolt transaction size is the current
	// mapped ledger, not the stale FileInfo captured when this owner opened
	// its file.
	if err := p.db.View(func(tx *bolt.Tx) error {
		digest, err := providerDigestTx(tx)
		if err != nil {
			return err
		}
		e.Digest, e.Bytes = digest, tx.Size()
		e.Claims, err = p.claimEvidence(tx)
		return err
	}); err != nil {
		return LedgerEvidence{}, err
	}
	if st, ok := p.file.Sys().(*syscall.Stat_t); ok {
		e.Device, e.Inode = uint64(st.Dev), uint64(st.Ino)
	}
	return e, nil
}
