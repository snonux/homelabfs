package controlledvalidation

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// SealTerminal serializes with actual execution and the ledger writer, checks
// every retained principal/claim/earmark, and protects this scope's irreversible
// seal. Existing complete authoritative records remain retained with the seal.
func (p *Provider) SealTerminal(ctx context.Context, a *guestbridge.TerminalAttempt) (guestbridge.TerminalSeal, error) {
	request, scope, err := a.Attempt(ctx)
	if err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	e := p.config.Authorization.Enrollment
	if scope != e.Scope || e.Protocol == nil || *e.Protocol != guestbridge.TerminalProtocol() {
		return guestbridge.TerminalSeal{}, fsapi.Permission
	}
	if err = p.Check(ctx, scope); err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	if !p.execution.TryLock() {
		return guestbridge.TerminalSeal{}, fsapi.WouldBlock
	}
	defer p.execution.Unlock()
	var seal guestbridge.TerminalSeal
	err = p.db.Update(func(tx *bolt.Tx) error {
		if err := p.terminalEmpty(tx); err != nil {
			return err
		}
		stateHash, err := p.terminalStateHash(tx)
		if err != nil {
			return err
		}
		seal, err = a.Seal(ctx, "PROVIDER", stateHash)
		if err != nil {
			return err
		}
		if seal.RetirementID != request.RetirementID {
			return fsapi.Stale
		}
		b := tx.Bucket([]byte(ledgerVersion))
		raw, err := json.Marshal(seal)
		if err != nil {
			return err
		}
		if old := b.Get([]byte("terminal-provider")); old != nil && string(old) != string(raw) {
			return fsapi.Stale
		}
		return b.Put([]byte("terminal-provider"), raw)
	})
	if err == nil {
		err = p.fail("terminal-provider-sealed")
	}
	return seal, err
}

// VerifyTerminalSeal reads the actual protected seal and rechecks its still-
// retained authoritative scope state. A supplied record alone never suffices.
func (p *Provider) VerifyTerminalSeal(ctx context.Context, a *guestbridge.TerminalAttempt, seal guestbridge.TerminalSeal) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	if scope != p.config.Authorization.Enrollment.Scope {
		return fsapi.Permission
	}
	return p.db.View(func(tx *bolt.Tx) error {
		raw, err := json.Marshal(seal)
		if err != nil {
			return err
		}
		if string(tx.Bucket([]byte(ledgerVersion)).Get([]byte("terminal-provider"))) != string(raw) {
			return fsapi.Stale
		}
		if err := p.terminalEmpty(tx); err != nil {
			return err
		}
		actual, err := p.terminalStateHash(tx)
		if err == nil && actual != seal.StateHash {
			return fsapi.Stale
		}
		return err
	})
}

// ProtectTerminalBridge retains the actual complete bridge lifetime snapshot
// and its seal in this same private owner. It does not admit new provider work.
func (p *Provider) ProtectTerminalBridge(ctx context.Context, a *guestbridge.TerminalAttempt, seal guestbridge.TerminalSeal, snapshot []byte) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	if scope != p.config.Authorization.Enrollment.Scope || seal.Scope != scope || seal.Owner != "BRIDGE" || hash(snapshot) != seal.StateHash || uint64(len(snapshot)) > p.config.MaxRetainedBytes {
		return fsapi.Permission
	}
	return p.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ledgerVersion))
		if b.Get([]byte("terminal-provider")) == nil {
			return fsapi.Permission
		}
		raw, err := json.Marshal(seal)
		if err != nil {
			return err
		}
		if old := b.Get([]byte("terminal-bridge")); old != nil {
			if string(old) != string(raw) || string(b.Get([]byte("terminal-bridge-snapshot"))) != string(snapshot) {
				return fsapi.Stale
			}
			return nil
		}
		if err = p.fail("terminal-bridge-protect"); err != nil {
			return err
		}
		if err = b.Put([]byte("terminal-bridge-snapshot"), snapshot); err != nil {
			return err
		}
		return b.Put([]byte("terminal-bridge"), raw)
	})
}

// VerifyTerminalBridge checks actual retained lifetime state, never a flag.
func (p *Provider) VerifyTerminalBridge(ctx context.Context, a *guestbridge.TerminalAttempt, seal guestbridge.TerminalSeal, snapshot []byte) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	if scope != p.config.Authorization.Enrollment.Scope {
		return fsapi.Permission
	}
	return p.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ledgerVersion))
		raw, err := json.Marshal(seal)
		if err != nil {
			return err
		}
		if string(b.Get([]byte("terminal-bridge"))) != string(raw) || string(b.Get([]byte("terminal-bridge-snapshot"))) != string(snapshot) || hash(snapshot) != seal.StateHash {
			return fsapi.Stale
		}
		return nil
	})
}

func (p *Provider) updateOpen(change func(*bolt.Tx) error) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket([]byte(ledgerVersion)).Get([]byte("terminal-provider")) != nil {
			return fsapi.Permission
		}
		return change(tx)
	})
}

func (p *Provider) terminalEmpty(tx *bolt.Tx) error {
	if err := p.bucket(tx, "principals").ForEach(func(_, raw []byte) error {
		var family principal
		if json.Unmarshal(raw, &family) != nil {
			return fsapi.Stale
		}
		if family.Floor != family.High {
			return fsapi.WouldBlock
		}
		return nil
	}); err != nil {
		return err
	}
	if err := p.bucket(tx, "claims").ForEach(func(_, raw []byte) error {
		var e entry
		if json.Unmarshal(raw, &e) != nil {
			return fsapi.Stale
		}
		owner := e.Binding.Context
		if e.Original != nil {
			owner = e.Original.Context
		}
		if owner != p.config.Authorization.Enrollment.Scope {
			return fsapi.Permission
		}
		ps, err := p.principal(tx, e.Claim)
		if err != nil {
			return err
		}
		if e.Claim.Number > ps.Floor || e.Status != "TERMINAL" && (e.Status != "DECLINED" || !e.Gap) || e.ReaddirClosure != nil && !e.ReaddirClosure.Closed {
			return fsapi.WouldBlock
		}
		return nil
	}); err != nil {
		return err
	}
	return p.bucket(tx, "earmarks").ForEach(func(_, raw []byte) error {
		var e Earmark
		if json.Unmarshal(raw, &e) != nil || e.Scope != p.config.Authorization.Enrollment.Scope {
			return fsapi.Stale
		}
		claim := numberClaim(e.Number)
		ps, err := p.principal(tx, claim)
		if err != nil {
			return err
		}
		if claim.Number > ps.Floor {
			return fsapi.WouldBlock
		}
		if e.Bound == nil {
			if !e.Closed || e.GapProof == "" {
				return fsapi.WouldBlock
			}
			return nil
		}
		// Closed denotes an unused earmark's protected cancellation. A
		// consumed earmark instead retains its exact completed drain claim;
		// it must not be rewritten as an unbound cancellation to close.
		claim.OriginalHash = e.Bound.Claim.OriginalHash
		if e.Closed || e.GapProof != "" || claim != e.Bound.Claim ||
			e.Bound.Intent.Opcode != "DRAIN_WRITE" || !equalJSON(e.Credentials, e.Bound.Credentials) {
			return fsapi.Stale
		}
		completed, ok, err := p.load(tx, claim)
		if err != nil {
			return err
		}
		if !ok || completed.Status != "TERMINAL" || completed.Gap ||
			completed.Binding.Context != e.Scope || !equalJSON(completed.Binding.Call, *e.Bound) ||
			!controlledResult(completed.Result) {
			return fsapi.WouldBlock
		}
		return nil
	})
}

func (p *Provider) terminalStateHash(tx *bolt.Tx) (string, error) {
	h := sha256.New()
	for _, name := range []string{"principals", "claims", "earmarks", "bridge", "processes", "retirements", "operations", "allocations", earmarkCreditBucket, readdirReceiptBucket} {
		_, _ = h.Write([]byte(name))
		if err := p.bucket(tx, name).ForEach(func(key, value []byte) error {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(key)))
			_, _ = h.Write(size[:])
			_, _ = h.Write(key)
			binary.BigEndian.PutUint64(size[:], uint64(len(value)))
			_, _ = h.Write(size[:])
			_, _ = h.Write(value)
			return nil
		}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
