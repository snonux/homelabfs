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

var _ guestbridge.ReaddirCleanupStore = (*Provider)(nil)

// CleanupReaddir spends the original page's preowned closure record. The only
// bridge changes are exact transient release bits in the current snapshot;
// neither source ownership nor ordinary claims/credits are created or freed.
func (p *Provider) CleanupReaddir(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	observed, ok := guestbridge.VerifiedRequest(ctx)
	protocol := guestbridge.CleanupProtocol()
	if enrolled := p.config.Authorization.Enrollment.Protocol; enrolled != nil {
		protocol = *enrolled
	}
	enrolled := p.config.Authorization.Enrollment
	if !ok || enrolled.Protocol == nil || !guestbridge.HasCleanupProtocol(protocol) || observed.Protocol == nil || *observed.Protocol != protocol || observed.Scope != scope || observed.Operation != "READDIR_CLEANUP" || observed.PayloadHash != hash(raw) || observed.Native.Endpoint != enrolled.Endpoint {
		return nil, fsapi.Permission
	}
	envelope, err := canonicalJSON(guestbridge.Command{Protocol: &protocol, Cell: observed.Cell, Context: scope, Op: "READDIR_CLEANUP", Payload: raw})
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, uint16(protocol.BridgeMinor)); err != nil {
		return nil, err
	}
	var request guestbridge.ReaddirCleanup
	if json.Unmarshal(raw, &request) != nil {
		return nil, fsapi.Invalid
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	var receipt json.RawMessage
	err = p.updateOpen(func(tx *bolt.Tx) error {
		parent, exists, err := p.load(tx, request.Claim)
		if err != nil {
			return err
		}
		if !exists || parent.Claim != request.Claim || parent.Binding.Context != scope || parent.Binding.Call.Intent.Opcode != "READDIR" || parent.Status != "TERMINAL" || parent.ReaddirClosure == nil || !bytes.Equal(parent.ReaddirClosure.Request, raw) {
			return fsapi.Permission
		}
		closure := parent.ReaddirClosure
		if closure.Closed {
			receipt = bytes.Clone(closure.Receipt)
			return nil
		}
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
		owner.SnapshotVersion, err = bridge.NextSequence()
		if err != nil {
			return err
		}
		if bytes.Equal(ownerKey, claimKey(parent.Claim)) {
			owner.ReaddirClosure.Closed = true
			parent = owner
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
		receipt = bytes.Clone(closure.Receipt)
		return p.fail("readdir-cleanup-before-commit")
	})
	return receipt, err
}
