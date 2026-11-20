package controlledvalidation

import (
	"encoding/json"
	"slices"
	"sync"

	bolt "go.etcd.io/bbolt"
)

// pendingIndex tracks which claim entries may be PENDING with a protected
// plan (task te4). checkPendingStage and CheckViewReady used to unmarshal
// every claims entry, bridge snapshots included, on each admission and each
// view check; in the zq3 run that was ~135 s of CPU. Both predicates require
// Status == "PENDING" && Plan != nil, so they now read only the entries this
// index names and apply their exact original predicate to the ledger bytes
// in their own transaction. The index is a hint, never evidence: a key that
// is no longer pending is simply skipped.
//
// Invariant: every committed claims entry with Status PENDING and a Plan has
// its key in candidates. It holds because
//   - New scans the whole committed claims bucket once (scanPendingIndex);
//   - every claims write goes through putClaim/deleteClaim, and a write that
//     makes an entry a candidate adds the key eagerly, before its transaction
//     commits (bbolt has one writer, so no other write can interleave and a
//     rolled-back add only leaves a harmless extra candidate);
//   - a key is removed only by the commit hook of a write that left it a
//     non-candidate, and only if no later write re-added it since (the
//     generation check), because bbolt runs commit hooks after releasing the
//     writer lock, when the next writer may already have re-added the key.
type pendingIndex struct {
	mu         sync.Mutex
	generation uint64
	candidates map[string]uint64
}

// pendingCandidate is the superset predicate of both exclusion scans.
func pendingCandidate(e entry) bool { return e.Status == "PENDING" && e.Plan != nil }

// putClaim stores one claims entry and maintains the pending index.
func (p *Provider) putClaim(tx *bolt.Tx, key []byte, e entry) error {
	if err := put(p.bucket(tx, "claims"), key, e); err != nil {
		return err
	}
	p.trackClaim(tx, string(key), pendingCandidate(e))
	return nil
}

// deleteClaim removes one claims entry and maintains the pending index.
func (p *Provider) deleteClaim(tx *bolt.Tx, key []byte) error {
	if err := p.bucket(tx, "claims").Delete(key); err != nil {
		return err
	}
	p.trackClaim(tx, string(key), false)
	return nil
}

func (p *Provider) trackClaim(tx *bolt.Tx, key string, candidate bool) {
	x := &p.pending
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.candidates == nil {
		x.candidates = map[string]uint64{}
	}
	if candidate {
		x.generation++
		x.candidates[key] = x.generation
		return
	}
	marked, ok := x.candidates[key]
	if !ok {
		return
	}
	tx.OnCommit(func() {
		x.mu.Lock()
		defer x.mu.Unlock()
		if x.candidates[key] == marked {
			delete(x.candidates, key)
		}
	})
}

// scanPendingIndex seeds the index from the committed ledger at open. It
// also unmarshals every entry once, so a malformed claims entry still
// refuses the provider before any admission.
func (p *Provider) scanPendingIndex() error {
	return p.db.View(func(tx *bolt.Tx) error {
		return p.bucket(tx, "claims").ForEach(func(key, raw []byte) error {
			var e entry
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			p.trackClaim(tx, string(key), pendingCandidate(e))
			return nil
		})
	})
}

// forEachPendingCandidate calls fn with each candidate entry as stored in
// tx, in key order. Keys absent from tx are skipped. The candidates cover
// every PENDING-with-plan entry of the latest committed state; a read
// transaction on an older snapshot may therefore miss an entry that was
// PENDING in its snapshot but has since settled (its removal hook already
// ran). That is harmless: the caller's verdict then matches the newer
// committed state, exactly as if it had started after that commit.
func (p *Provider) forEachPendingCandidate(tx *bolt.Tx, fn func(entry) error) error {
	p.pending.mu.Lock()
	keys := make([]string, 0, len(p.pending.candidates))
	for key := range p.pending.candidates {
		keys = append(keys, key)
	}
	p.pending.mu.Unlock()
	slices.Sort(keys)
	bucket := p.bucket(tx, "claims")
	for _, key := range keys {
		raw := bucket.Get([]byte(key))
		if raw == nil {
			continue
		}
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}
