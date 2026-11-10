package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

type retireRequest struct {
	Realm, Subject, Anchor, Nonce string
	Through                       uint64
	Hashes                        []string `json:"result_hashes"`
}

// Retire advances a durable permanent floor only across complete exact terminal
// result or protected gap coverage. No allocation, timeout or absence is skipped.
// The same transaction recycles the newly retired numbers and prunes the bridge
// snapshot's retired tombstones (recycle.go); a replay of an exact request
// returns its protected receipt before reading any entry.
func (p *Provider) Retire(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	var req retireRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	claim := guestbridge.Claim{Realm: req.Realm, Subject: req.Subject, Anchor: req.Anchor, Nonce: req.Nonce}
	status := "BLOCKED"
	var floor uint64
	var proof any
	var receipt []byte
	receiptKey := []byte(hash(raw))
	err := p.updateOpen(func(tx *bolt.Tx) error {
		if old := p.bucket(tx, "retirements").Get(receiptKey); old != nil {
			receipt = bytes.Clone(old)
			return nil
		}
		ps, covered, err := p.retireCovered(tx, scope, claim, req)
		floor = ps.Floor
		if err != nil || !covered {
			return err
		}
		if floor, err = p.advanceFloor(tx, scope, claim, ps, req.Through); err != nil {
			return err
		}
		status = "RETIRED"
		proof = hash(append(bytes.Clone(raw), []byte(ledgerVersion)...))
		receipt, err = json.Marshal(map[string]any{"status": status, "floor": floor, "proof_hash": proof})
		if err != nil {
			return err
		}
		return p.bucket(tx, "retirements").Put(receiptKey, receipt)
	})
	if err != nil {
		return nil, err
	}
	if len(receipt) > 0 {
		return receipt, nil
	}
	return json.Marshal(map[string]any{"status": status, "floor": floor, "proof_hash": proof})
}

// retireCovered reports whether req exactly covers floor+1..Through with
// terminal result ids or protected gap proofs. Not covered answers BLOCKED.
func (p *Provider) retireCovered(tx *bolt.Tx, scope guestbridge.Context, claim guestbridge.Claim, req retireRequest) (principal, bool, error) {
	ps, err := p.principal(tx, claim)
	if err != nil {
		return ps, false, err
	}
	floor := ps.Floor
	if req.Through <= floor || req.Through > ps.High || req.Through-floor > 1024 || uint64(len(req.Hashes)) != req.Through-floor {
		return ps, false, nil
	}
	for n := floor + 1; n <= req.Through; n++ {
		claim.Number = n
		expected, ok, err := p.coverageHash(tx, scope, claim)
		if err != nil || !ok || req.Hashes[n-floor-1] != expected {
			return ps, false, err
		}
	}
	return ps, true, nil
}

// coverageHash is the hash a retirement must name for one number: a terminal
// result's id (with any READDIR cleanup closed), a protected gap entry's
// hash, or a closed unbound earmark's gap proof. ok false means uncovered.
func (p *Provider) coverageHash(tx *bolt.Tx, scope guestbridge.Context, claim guestbridge.Claim) (string, bool, error) {
	e, ok, err := p.load(tx, claim)
	if err != nil {
		return "", false, err
	}
	if !ok {
		raw := p.bucket(tx, "earmarks").Get(claimKey(claim))
		if raw == nil {
			return "", false, nil
		}
		var earmark Earmark
		if err := json.Unmarshal(raw, &earmark); err != nil {
			return "", false, err
		}
		if !earmark.Closed || earmark.Scope != scope || earmark.Bound != nil {
			return "", false, nil
		}
		if len(earmark.GapProof) != 64 {
			return "", false, fsapi.Stale
		}
		return earmark.GapProof, true, nil
	}
	switch {
	case e.Status == "TERMINAL":
		if e.ReaddirClosure != nil && !e.ReaddirClosure.Closed {
			return "", false, nil
		}
		var r struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(e.Result, &r); err != nil {
			return "", false, err
		}
		return r.ID, true, nil
	case e.Gap && e.Status == "DECLINED":
		b, err := json.Marshal(e)
		if err != nil {
			return "", false, err
		}
		return hash(b), true, nil
	}
	return "", false, nil
}

// advanceFloor moves the family's floor to through, prunes the bridge
// snapshot's tombstones released at or below it and recycles the retired
// numbers, all in the retirement's own transaction.
func (p *Provider) advanceFloor(tx *bolt.Tx, scope guestbridge.Context, claim guestbridge.Claim, ps principal, through uint64) (uint64, error) {
	ps.Floor = through
	if err := p.pruneRetiredSnapshot(tx, scope, claim, through); err != nil {
		return 0, err
	}
	if err := p.recycleRetired(tx, scope, claim, &ps); err != nil {
		return 0, err
	}
	return ps.Floor, p.putPrincipal(tx, claim, ps)
}

// retired reports whether claim's number is at or below its family's floor.
func (p *Provider) retired(tx *bolt.Tx, claim guestbridge.Claim) (bool, error) {
	ps, err := p.principal(tx, claim)
	return claim.Number <= ps.Floor, err
}
