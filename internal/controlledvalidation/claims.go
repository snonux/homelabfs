package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// Allocate protects the next exact public number before it can be sent. This
// trusted local allocator is separate from guest CALL admission. Unsent numbers
// remain explicit unresolved entries until ResolveGap protects their no-effect.
func (p *Provider) Allocate(ctx context.Context, scope guestbridge.Context, claim guestbridge.Claim) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	if !validClaim(claim) {
		return fsapi.Invalid
	}
	return p.allocate(claim, nil)
}

func (p *Provider) allocate(claim guestbridge.Claim, original *guestbridge.Binding) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		if p.bucket(tx, "earmarks").Get(claimKey(claim)) != nil {
			return fsapi.Permission
		}
		b := p.bucket(tx, "claims")
		k := claimKey(claim)
		if old := b.Get(k); old != nil {
			var e entry
			if err := json.Unmarshal(old, &e); err != nil {
				return err
			}
			if e.Claim != claim || original != nil && (e.Original == nil || !sameExact(*e.Original, *original)) {
				return fsapi.Stale
			}
			return nil
		}
		if p.identityCount(tx) >= p.config.MaxClaims {
			return fsapi.NoSpace
		}
		ps, err := p.principal(tx, claim)
		if err != nil {
			return err
		}
		if ps.High == ^uint64(0) || claim.Number != ps.High+1 {
			return fsapi.Stale
		}
		ps.High = claim.Number
		if err = p.putPrincipal(tx, claim, ps); err != nil {
			return err
		}
		e := entry{Version: ledgerVersion, Claim: claim, Status: "ALLOCATED", Original: original}
		if original != nil {
			raw, err := json.Marshal(e)
			if err != nil {
				return err
			}
			if uint64(len(raw)) > p.config.ClaimCreditBytes {
				return fsapi.NoSpace
			}
			if err = p.reserveCredit(tx, "retained-credit", p.config.ClaimCreditBytes, p.config.MaxRetainedBytes); err != nil {
				return err
			}
			if err = p.reserveCredit(tx, "source-credit", p.config.SourceCreditBytes, p.config.MaxSourceBytes); err != nil {
				return err
			}
			e.RetainedCredit, e.SourceCredit = p.config.ClaimCreditBytes, p.config.SourceCreditBytes
		}
		return put(b, k, e)
	})
}

// Resolve authenticates scope and immutable anchor before exposing old bytes.
func (p *Provider) Resolve(ctx context.Context, scope guestbridge.Context, claim guestbridge.Claim) (guestbridge.ClaimState, error) {
	if err := p.Check(ctx, scope); err != nil {
		return guestbridge.ClaimState{}, err
	}
	var out guestbridge.ClaimState
	err := p.db.View(func(tx *bolt.Tx) error {
		ps, err := p.principal(tx, claim)
		if err != nil {
			return err
		}
		if claim.Number <= ps.Floor {
			out.Status = "RETIRED_ID"
			return nil
		}
		e, ok, err := p.load(tx, claim)
		if err != nil {
			return err
		}
		if !ok || e.Status == "ALLOCATED" {
			out.Status = "UNKNOWN"
			return nil
		}
		if e.Claim != claim {
			return fsapi.Stale
		}
		out = state(e)
		if p.protectedGapQuery(ctx, scope, e) {
			out.Status = "DECLINED"
		}
		return nil
	})
	return out, err
}

// Reserve persists immutable canonical credentials, intent, context, UUID and
// allocation before planning. It cannot authorize an unallocated guest number.
func (p *Provider) Reserve(ctx context.Context, b guestbridge.Binding) (guestbridge.ClaimState, error) {
	if err := p.Check(ctx, b.Context); err != nil {
		return guestbridge.ClaimState{}, err
	}
	intent, err := canonicalJSON(b.Call.Intent)
	if err != nil || hash(intent) != b.Call.Claim.OriginalHash {
		return guestbridge.ClaimState{}, fsapi.Invalid
	}
	canonical, err := canonicalCall(b.Call)
	if err != nil || !bytes.Equal(canonical, b.Canonical) {
		return guestbridge.ClaimState{}, fsapi.Invalid
	}
	var out guestbridge.ClaimState
	err = p.db.Update(func(tx *bolt.Tx) error {
		ps, err := p.principal(tx, b.Call.Claim)
		if err != nil {
			return err
		}
		if b.Call.Claim.Number <= ps.Floor {
			return fsapi.Stale
		}
		e, ok, err := p.load(tx, b.Call.Claim)
		if err != nil {
			return err
		}
		if !ok || e.Gap || e.Claim != b.Call.Claim || e.Original != nil && !sameBinding(*e.Original, b) {
			return fsapi.Permission
		}
		if e.Status != "ALLOCATED" {
			if !sameBinding(e.Binding, b) {
				return fsapi.Stale
			}
			out = state(e)
			return nil
		}
		if err = p.checkPendingStage(tx, b.Context, fsapi.UUID{}); err != nil {
			return err
		}
		b.Operation, err = p.reserveUUID(tx, "operations")
		if err != nil {
			return err
		}
		switch b.Call.Intent.Opcode {
		case "CREATE", "MKDIR", "SYMLINK":
			id, e := p.reserveUUID(tx, "allocations")
			if e != nil {
				return e
			}
			home, e := fsapi.ParseUUID(b.Context.Volume)
			if e != nil {
				return e
			}
			b.Allocation = fsapi.ObjectID{Home: home, Allocation: id}
		}
		if e.RetainedCredit == 0 {
			if err = p.reserveCredit(tx, "retained-credit", p.config.ClaimCreditBytes, p.config.MaxRetainedBytes); err != nil {
				return err
			}
			if err = p.reserveCredit(tx, "source-credit", p.config.SourceCreditBytes, p.config.MaxSourceBytes); err != nil {
				return err
			}
		}
		e.RetainedCredit = p.config.ClaimCreditBytes
		e.SourceCredit = p.config.SourceCreditBytes
		e.Binding = b
		e.Status = "RESERVED"
		e.PreparationStage = "UNPREPARED"
		if err = put(p.bucket(tx, "claims"), claimKey(e.Claim), e); err != nil {
			return err
		}
		out = state(e)
		return nil
	})
	return out, err
}

// ProtectResult persists exact completed wire bytes before TERMINAL is visible.
func (p *Provider) ProtectResult(ctx context.Context, b guestbridge.Binding, result json.RawMessage) error {
	if err := p.Check(ctx, b.Context); err != nil {
		return err
	}
	if !controlledResult(result) {
		return fsapi.Invalid
	}
	return p.update(b, func(e *entry) error {
		if e.Outcome == nil {
			return fsapi.Unavailable
		}
		if len(e.Result) != 0 && !bytes.Equal(e.Result, result) {
			return fsapi.Stale
		}
		e.Result = bytes.Clone(result)
		e.Status = "TERMINAL"
		return nil
	})
}

// Cancel only resolves protected allocated/reserved no-effect claims. Pending
// execution is never converted into absence by timeout or cancellation.
func (p *Provider) Cancel(ctx context.Context, scope guestbridge.Context, claim guestbridge.Claim) (guestbridge.ClaimState, error) {
	if err := p.Check(ctx, scope); err != nil {
		return guestbridge.ClaimState{}, err
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	err := p.db.Update(func(tx *bolt.Tx) error {
		if raw := p.bucket(tx, "earmarks").Get(claimKey(claim)); raw != nil {
			var earmark Earmark
			if err := json.Unmarshal(raw, &earmark); err != nil {
				return err
			}
			if earmark.Bound != nil && !earmark.Closed {
				return fsapi.WouldBlock
			}
		}
		e, ok, err := p.load(tx, claim)
		if err != nil {
			return err
		}
		if !ok || e.Claim != claim {
			return fsapi.Permission
		}
		if e.Status == "ALLOCATED" || e.Status == "RESERVED" {
			if e.Original != nil && e.PreparationStage != "" && e.PreparationStage != "UNPREPARED" {
				return fsapi.Permission
			}
			e.Gap = true
			e.Status = "DECLINED"
			return put(p.bucket(tx, "claims"), claimKey(claim), e)
		}
		return nil
	})
	if err != nil {
		return guestbridge.ClaimState{}, err
	}
	return p.Resolve(ctx, scope, claim)
}

// ResolveGap protects authenticated no-effect for an allocated, never admitted
// number. It refuses every pending or terminal operation.
func (p *Provider) ResolveGap(ctx context.Context, scope guestbridge.Context, claim guestbridge.Claim) (string, error) {
	if err := p.Check(ctx, scope); err != nil {
		return "", err
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	var proof string
	err := p.db.Update(func(tx *bolt.Tx) error {
		if raw := p.bucket(tx, "earmarks").Get(claimKey(claim)); raw != nil {
			var earmark Earmark
			if err := json.Unmarshal(raw, &earmark); err != nil {
				return err
			}
			if earmark.Bound != nil && !earmark.Closed {
				return fsapi.WouldBlock
			}
		}
		e, ok, err := p.load(tx, claim)
		if err != nil {
			return err
		}
		if !ok || e.Claim != claim || e.Plan != nil || e.Outcome != nil || len(e.Result) != 0 || e.Original != nil && e.PreparationStage != "" && e.PreparationStage != "UNPREPARED" {
			return fsapi.Permission
		}
		if e.Status != "ALLOCATED" && e.Status != "RESERVED" && e.Status != "DECLINED" {
			return fsapi.Unavailable
		}
		e.Gap = true
		e.Status = "DECLINED"
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		proof = hash(raw)
		return put(p.bucket(tx, "claims"), claimKey(claim), e)
	})
	return proof, err
}

// Retire advances a durable permanent floor only across complete exact terminal
// result or protected gap coverage. No allocation, timeout or absence is skipped.
func (p *Provider) Retire(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	var req struct {
		Realm, Subject, Anchor, Nonce string
		Through                       uint64
		Hashes                        []string `json:"result_hashes"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	claim := guestbridge.Claim{Realm: req.Realm, Subject: req.Subject, Anchor: req.Anchor, Nonce: req.Nonce}
	status := "BLOCKED"
	var floor uint64
	var proof any
	var receipt []byte
	receiptKey := []byte(hash(raw))
	err := p.db.Update(func(tx *bolt.Tx) error {
		if old := p.bucket(tx, "retirements").Get(receiptKey); old != nil {
			receipt = bytes.Clone(old)
			return nil
		}
		ps, err := p.principal(tx, claim)
		if err != nil {
			return err
		}
		floor = ps.Floor
		if req.Through <= floor || req.Through > ps.High || req.Through-floor > 1024 || uint64(len(req.Hashes)) != req.Through-floor {
			return nil
		}
		for n := floor + 1; ; n++ {
			claim.Number = n
			e, ok, err := p.load(tx, claim)
			if err != nil {
				return err
			}
			var expected string
			if !ok {
				raw := p.bucket(tx, "earmarks").Get(claimKey(claim))
				if raw == nil {
					return nil
				}
				var earmark Earmark
				if err := json.Unmarshal(raw, &earmark); err != nil {
					return err
				}
				if !earmark.Closed || earmark.Scope != scope || earmark.Bound != nil {
					return nil
				}
				expected = earmark.GapProof
				if len(expected) != 64 {
					return fsapi.Stale
				}
			}

			if !ok {
				// Exact typed unbound gap proof was checked above.
			} else if e.Status == "TERMINAL" {
				if e.ReaddirClosure != nil && !e.ReaddirClosure.Closed {
					return nil
				}
				var r struct {
					ID string `json:"id"`
				}
				if err = json.Unmarshal(e.Result, &r); err != nil {
					return err
				}
				expected = r.ID
			} else if e.Gap && e.Status == "DECLINED" {
				b, err := json.Marshal(e)
				if err != nil {
					return err
				}
				expected = hash(b)
			} else {
				return nil
			}
			if req.Hashes[n-floor-1] != expected {
				return nil
			}
			if n == req.Through {
				break
			}
		}
		ps.Floor = req.Through
		if err = p.putPrincipal(tx, claim, ps); err != nil {
			return err
		}
		floor = ps.Floor
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
