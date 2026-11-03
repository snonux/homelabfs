package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// Finalize atomically protects exact wire result and complete capability state.
func (p *Provider) Finalize(ctx context.Context, b guestbridge.Binding, result, snapshot json.RawMessage) error {
	if err := p.Check(ctx, b.Context); err != nil {
		return err
	}
	if !controlledResult(result) || !json.Valid(snapshot) {
		return fsapi.Invalid
	}
	return p.db.Update(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, b.Call.Claim)
		if err != nil {
			return err
		}
		if !ok || !sameExact(e.Binding, b) || e.Outcome == nil {
			return fsapi.Stale
		}
		if len(e.Result) != 0 {
			if !bytes.Equal(e.Result, result) {
				return fsapi.Stale
			}
			return nil
		}
		e.Result = bytes.Clone(result)
		e.Status = "TERMINAL"
		if err = put(p.bucket(tx, "claims"), claimKey(e.Claim), e); err != nil {
			return err
		}
		e.BridgeState = bytes.Clone(snapshot)
		if e.SnapshotVersion == 0 {
			e.SnapshotVersion, err = p.bucket(tx, "bridge").NextSequence()
			if err != nil {
				return err
			}
		}
		if err = put(p.bucket(tx, "claims"), claimKey(e.Claim), e); err != nil {
			return err
		}
		return p.protectSnapshot(tx, e)
	})
}

// Restore returns only an authenticated same-scope protected bridge snapshot.
// The bridge validates its controller, generation, limits and grant bindings.
func (p *Provider) Restore(ctx context.Context, scope guestbridge.Context) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	var raw []byte
	err := p.db.View(func(tx *bolt.Tx) error {
		raw = bytes.Clone(p.bucket(tx, "bridge").Get([]byte(scope.Session)))
		return nil
	})
	return raw, err
}
