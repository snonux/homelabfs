package controlledvalidation

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// This tests the retained-ledger closure predicate only. Seeded records cannot
// issue a seal, source or receipt; the actual C/Go dirty test covers that path.
func TestTerminalConsumedEarmarkCut(t *testing.T) {
	for _, name := range []string{"completed", "open-unused", "closed-unused", "missing-gap-proof", "missing-claim", "pending-claim", "wrong-bound-number", "wrong-credential", "wrong-opcode", "bound-cancelled", "bound-gap", "floor-behind", "nonlocal-result"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			b := f.binding("DRAIN_WRITE", map[string]any{})
			c := b.Call.Claim
			n := guestbridge.ClaimNumber{Realm: c.Realm, Subject: c.Subject, Anchor: c.Anchor, Nonce: c.Nonce, Number: c.Number}
			e := Earmark{Scope: b.Context, Number: n, Credentials: b.Call.Credentials, Bound: &b.Call}
			completed := entry{Version: ledgerVersion, Claim: c, Binding: b, Status: "TERMINAL", Result: json.RawMessage(`{"publication":null}`)}
			floor := c.Number
			pass := name == "completed" || name == "closed-unused"
			switch name {
			case "open-unused":
				e.Bound = nil
			case "closed-unused":
				e.Bound = nil
				e.Closed = true
				e.GapProof = hash([]byte("protected cancellation fixture"))
			case "missing-gap-proof":
				e.Bound = nil
				e.Closed = true
			case "pending-claim":
				completed.Status = "PENDING"
			case "wrong-bound-number":
				e.Number.Number++
			case "wrong-credential":
				e.Credentials.Groups = []uint32{201}
			case "wrong-opcode":
				copy := *e.Bound
				copy.Intent.Opcode = "WRITE"
				e.Bound = &copy
			case "bound-cancelled":
				e.Closed = true
			case "bound-gap":
				e.GapProof = hash([]byte("not a consumed cancellation"))
			case "floor-behind":
				floor--
			case "nonlocal-result":
				completed.Result = json.RawMessage(`{"publication":{}}`)
			}
			mustAllocation(t, f.provider.db.Update(func(tx *bolt.Tx) error {
				if err := f.provider.putPrincipal(tx, c, principal{High: c.Number, Floor: floor}); err != nil {
					return err
				}
				if name == "missing-claim" {
					if err := f.provider.bucket(tx, "claims").Delete(claimKey(c)); err != nil {
						return err
					}
				} else if err := put(f.provider.bucket(tx, "claims"), claimKey(c), completed); err != nil {
					return err
				}
				return put(f.provider.bucket(tx, "earmarks"), claimKey(c), e)
			}))
			err := f.provider.db.View(f.provider.terminalEmpty)
			if (err == nil) != pass {
				t.Fatalf("terminal retained cut accepted=%t want%t: %v", err == nil, pass, err)
			}
		})
	}
}
