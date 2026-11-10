package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// CheckRestoredBridge reconciles the complete C8 state with this actual private
// provider's protected outcome/terminal snapshot. It neither imports state nor
// grants native continuity. Missing state and disagreement refuse restoration.
func (p *Provider) CheckRestoredBridge(ctx context.Context, scope guestbridge.Context, controller fsapi.UUID, raw json.RawMessage) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	var complete map[string]json.RawMessage
	if err := json.Unmarshal(raw, &complete); err != nil {
		return err
	}
	return p.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ledgerVersion))
		terminal := b.Get([]byte("terminal-bridge"))
		if len(terminal) > 0 {
			var seal guestbridge.TerminalSeal
			if json.Unmarshal(terminal, &seal) != nil || seal.Scope != scope || seal.Controller != controller.String() || seal.Owner != "BRIDGE" || !sameDecodedJSON(complete["Terminal"], terminal) {
				return fsapi.Stale
			}
			delete(complete, "Terminal")
			restored, err := json.Marshal(complete)
			if err != nil {
				return err
			}
			saved := b.Get([]byte("terminal-bridge-snapshot"))
			if len(saved) == 0 || !sameDecodedJSON(restored, saved) || hash(saved) != seal.StateHash {
				return fsapi.Stale
			}
			return p.terminalEmpty(tx)
		}
		if value := complete["Terminal"]; len(value) > 0 && string(value) != "null" {
			return fsapi.Stale
		}
		saved := p.bucket(tx, "bridge").Get([]byte(scope.Session))
		if len(saved) == 0 {
			for _, key := range []string{"Grants", "Credentials", "Exited", "CloseSequences"} {
				var entries map[string]any
				if json.Unmarshal(complete[key], &entries) != nil || len(entries) != 0 {
					return fsapi.Stale
				}
			}
			// No provider-generated capability has existed in this empty owner.
			return nil
		}
		return p.checkRestoredSnapshot(tx, controller, saved, complete, raw)
	})
}

// checkRestoredSnapshot compares the restored bridge row with the protected
// snapshot. Grants and CloseSequences may differ only by tombstones that
// retirement prunes (o64): a row persisted before a pruning Retire's reply (a
// crash between the provider commit and the bridge persist) still holds them.
//
// The comparison prunes both sides at every family's floor (u64). Pruning
// only the row is not enough for a multi-family session: Retire prunes the
// snapshot at the retiring family's floor alone, and pruneRetiredSnapshot
// skips the prune when the snapshot owner's entry does not match the
// snapshot, so the snapshot can still hold a tombstone at another family's
// floor. The all-floor row prune drops that tombstone, and a row saved in a
// later family's retire window would be refused Stale.
//
// Integrity: the floors come only from this provider's own principals,
// read in the caller's transaction; pruneGrantRows drops only Released
// grants owned by a claim at or below its family's floor, and their close
// sequences. A missing, extra or changed live grant, or a live grant marked
// released on one side only, survives the prune on at least one side and so
// still compares Stale.
func (p *Provider) checkRestoredSnapshot(tx *bolt.Tx, controller fsapi.UUID, saved []byte, complete map[string]json.RawMessage, raw json.RawMessage) error {
	var snapshot map[string]json.RawMessage
	if json.Unmarshal(saved, &snapshot) != nil {
		return fsapi.Stale
	}
	var savedController fsapi.UUID
	if json.Unmarshal(snapshot["Controller"], &savedController) != nil || savedController != controller {
		return fsapi.Stale
	}
	for _, key := range []string{"Context", "Generation", "Credentials", "Exited"} {
		if !sameDecodedJSON(snapshot[key], complete[key]) {
			return fsapi.Stale
		}
	}
	if sameDecodedJSON(snapshot["Grants"], complete["Grants"]) && sameDecodedJSON(snapshot["CloseSequences"], complete["CloseSequences"]) {
		return nil
	}
	floors, err := p.familyFloors(tx)
	if err != nil {
		return err
	}
	savedGrants, savedCloses, err := guestbridge.PruneRetiredSession(saved, floors)
	if err != nil {
		return fsapi.Stale
	}
	grants, closes, err := guestbridge.PruneRetiredSession(raw, floors)
	if err != nil || !sameDecodedJSON(savedGrants, grants) || !sameDecodedJSON(savedCloses, closes) {
		return fsapi.Stale
	}
	return nil
}

// familyFloors lists every claim family's retirement floor.
func (p *Provider) familyFloors(tx *bolt.Tx) ([]guestbridge.ClaimNumber, error) {
	var floors []guestbridge.ClaimNumber
	err := p.bucket(tx, "principals").ForEach(func(key, value []byte) error {
		var names []string
		var family principal
		if json.Unmarshal(key, &names) != nil || len(names) != 4 || json.Unmarshal(value, &family) != nil {
			return fsapi.Stale
		}
		floors = append(floors, guestbridge.ClaimNumber{Realm: names[0], Subject: names[1], Anchor: names[2], Nonce: names[3], Number: family.Floor})
		return nil
	})
	return floors, err
}

func sameDecodedJSON(a, b []byte) bool {
	var left, right any
	decode := func(raw []byte, dst *any) error {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		return d.Decode(dst)
	}
	return decode(a, &left) == nil && decode(b, &right) == nil && reflect.DeepEqual(left, right)
}

// CheckRestoredTerminal matches C8's complete seal set against this actual
// provider's permanent seal and bridge snapshot, not a detached signature.
func (p *Provider) CheckRestoredTerminal(ctx context.Context, scope guestbridge.Context, seals []guestbridge.TerminalSeal) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	return p.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(ledgerVersion))
		for _, owner := range []string{"PROVIDER", "BRIDGE"} {
			key := "terminal-provider"
			if owner == "BRIDGE" {
				key = "terminal-bridge"
			}
			var supplied *guestbridge.TerminalSeal
			for i := range seals {
				if seals[i].Owner == owner {
					if supplied != nil {
						return fsapi.Stale
					}
					supplied = &seals[i]
				}
			}
			if supplied == nil {
				return fsapi.Stale
			}
			raw, err := json.Marshal(supplied)
			if err != nil {
				return err
			}
			if !sameDecodedJSON(raw, b.Get([]byte(key))) {
				return fsapi.Stale
			}
			if owner == "PROVIDER" {
				digest, err := p.terminalStateHash(tx)
				if err != nil {
					return err
				}
				if digest != supplied.StateHash {
					return fsapi.Stale
				}
			}
		}
		return p.terminalEmpty(tx)
	})
}
