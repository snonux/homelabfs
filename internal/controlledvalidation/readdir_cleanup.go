package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// The ordinary 4096-byte admission allowance includes the maximum decimal
// growth of a uint64 snapshot sequence. Cleanup consumes that preowned slot.
const readdirSnapshotSequenceBytes = 20

// readdirReceiptBucket keeps a recycled READDIR's closed cleanup receipt
// (o64): retirement recycling deletes the READDIR entry that held it, and a
// replayed READDIR_CLEANUP must still get its original receipt, not a refusal.
const readdirReceiptBucket = "readdir-receipts"

type readdirReceipt struct {
	RequestHash string
	Receipt     json.RawMessage
}

var _ guestbridge.ReaddirCleanupStore = (*Provider)(nil)

// CleanupReaddir spends the original page's preowned closure record. The only
// bridge changes are exact transient release bits in the current snapshot;
// neither source ownership nor ordinary claims/credits are created or freed.
func (p *Provider) CleanupReaddir(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	request, err := p.cleanupRequest(ctx, scope, raw)
	if err != nil {
		return nil, err
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	var receipt json.RawMessage
	err = p.updateOpen(func(tx *bolt.Tx) error {
		var e error
		receipt, e = p.cleanupReaddir(tx, scope, request, raw)
		return e
	})
	return receipt, err
}

// cleanupRequest authenticates the exact verified READDIR_CLEANUP command.
func (p *Provider) cleanupRequest(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (guestbridge.ReaddirCleanup, error) {
	var request guestbridge.ReaddirCleanup
	observed, ok := guestbridge.VerifiedRequest(ctx)
	protocol := guestbridge.CleanupProtocol()
	if enrolled := p.config.Authorization.Enrollment.Protocol; enrolled != nil {
		protocol = *enrolled
	}
	enrolled := p.config.Authorization.Enrollment
	if !ok || enrolled.Protocol == nil || !guestbridge.HasCleanupProtocol(protocol) || observed.Protocol == nil || *observed.Protocol != protocol || observed.Scope != scope || observed.Operation != "READDIR_CLEANUP" || observed.PayloadHash != hash(raw) || observed.Native.Endpoint != enrolled.Endpoint {
		return request, fsapi.Permission
	}
	envelope, err := canonicalJSON(guestbridge.Command{Protocol: &protocol, Cell: observed.Cell, Context: scope, Op: "READDIR_CLEANUP", Payload: raw})
	if err != nil {
		return request, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, uint16(protocol.BridgeMinor)); err != nil {
		return request, err
	}
	if json.Unmarshal(raw, &request) != nil {
		return request, fsapi.Invalid
	}
	return request, nil
}

// cleanupReaddir closes the page's closure once: a closed (or recycled)
// closure replays its exact receipt; otherwise the snapshot owner's release
// bits are rewritten and the closure is marked closed.
func (p *Provider) cleanupReaddir(tx *bolt.Tx, scope guestbridge.Context, request guestbridge.ReaddirCleanup, raw json.RawMessage) (json.RawMessage, error) {
	parent, exists, err := p.load(tx, request.Claim)
	if err != nil {
		return nil, err
	}
	if !exists {
		return p.keptReaddirReceipt(tx, request.Claim, raw)
	}
	if parent.Claim != request.Claim || parent.Binding.Context != scope || parent.Binding.Call.Intent.Opcode != "READDIR" || parent.Status != "TERMINAL" || parent.ReaddirClosure == nil || !bytes.Equal(parent.ReaddirClosure.Request, raw) {
		return nil, fsapi.Permission
	}
	closure := parent.ReaddirClosure
	if closure.Closed {
		return bytes.Clone(closure.Receipt), nil
	}
	if err = p.releaseReaddirGrants(tx, scope, request, parent); err != nil {
		return nil, err
	}
	return bytes.Clone(closure.Receipt), nil
}

// releaseReaddirGrants rewrites the protected snapshot owner's transient
// release bits and closes parent's closure in the same transaction.
func (p *Provider) releaseReaddirGrants(tx *bolt.Tx, scope guestbridge.Context, request guestbridge.ReaddirCleanup, parent entry) error {
	bridge := p.bucket(tx, "bridge")
	snapshot := bridge.Get([]byte(scope.Session))
	ownerKey := bridge.Get([]byte(scope.Session + "/owner"))
	if snapshot == nil || ownerKey == nil {
		return fsapi.Unavailable
	}
	var owner entry
	ownerRaw := p.bucket(tx, "claims").Get(ownerKey)
	if ownerRaw == nil || json.Unmarshal(ownerRaw, &owner) != nil || owner.Version != ledgerVersion || !bytes.Equal(ownerKey, claimKey(owner.Claim)) || owner.Status != "TERMINAL" || owner.Binding.Context != scope || !bytes.Equal(owner.BridgeState, snapshot) {
		return fsapi.Stale
	}
	updated, err := guestbridge.ApplyReaddirCleanup(scope, snapshot, request)
	if err != nil {
		return err
	}
	owner.BridgeState = updated
	if owner.SnapshotVersion, err = bridge.NextSequence(); err != nil {
		return err
	}
	if bytes.Equal(ownerKey, claimKey(parent.Claim)) {
		owner.ReaddirClosure.Closed = true
	} else {
		parent.ReaddirClosure.Closed = true
		if err = put(p.bucket(tx, "claims"), claimKey(parent.Claim), parent); err != nil {
			return err
		}
	}
	// Replacing an already funded snapshot must not grow its owner's entry.
	encoded, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	// A sequence may gain decimal digits; its fixed 64-bit slot was included
	// in the existing 4096-byte per-entry admission allowance.
	if len(encoded) > len(ownerRaw)+readdirSnapshotSequenceBytes || uint64(len(encoded)+len(updated)+4096-readdirSnapshotSequenceBytes) > owner.RetainedCredit {
		return fsapi.NoSpace
	}
	if err = put(p.bucket(tx, "claims"), ownerKey, owner); err != nil {
		return err
	}
	if err = p.protectSnapshot(tx, owner); err != nil {
		return err
	}
	return p.fail("readdir-cleanup-before-commit")
}

// keptReaddirReceipt answers a cleanup of a READDIR without an entry: only a
// recycled READDIR whose closed receipt was kept for this exact request
// replays it; anything else stays the old Permission refusal.
func (p *Provider) keptReaddirReceipt(tx *bolt.Tx, claim guestbridge.Claim, raw json.RawMessage) (json.RawMessage, error) {
	stored := p.bucket(tx, readdirReceiptBucket).Get(claimKey(claim))
	var kept readdirReceipt
	if stored == nil || json.Unmarshal(stored, &kept) != nil || kept.RequestHash != hash(raw) {
		return nil, fsapi.Permission
	}
	return bytes.Clone(kept.Receipt), nil
}

// keepReaddirReceipt is called by recycling before a READDIR entry with a
// closed cleanup closure is deleted.
func (p *Provider) keepReaddirReceipt(tx *bolt.Tx, key []byte, e entry) error {
	if e.ReaddirClosure == nil || !e.ReaddirClosure.Closed {
		return nil
	}
	return put(p.bucket(tx, readdirReceiptBucket), key, readdirReceipt{hash(e.ReaddirClosure.Request), e.ReaddirClosure.Receipt})
}
