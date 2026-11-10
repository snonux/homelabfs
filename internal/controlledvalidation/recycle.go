package controlledvalidation

import (
	"bytes"
	"encoding/binary"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// Retirement recycling (task o64).
//
// Before o64 nothing ever returned a credit or deleted a claim entry, so the
// provider budgets were lifetime budgets: every CALL, reads included, kept
// SourceCreditBytes and ClaimCreditBytes for the life of the session. At the
// zq3 spec (16 MiB / 1 MiB) the 17th CALL of a domain got NoSpace, early in
// T1. Each entry also kept its full bridge snapshot, and Reserve scans every
// entry, so the cost per CALL grew with the session.
//
// Now a successful Retire recycles every number at or below the new floor:
// its claim entry and earmark are deleted and its credits return to the
// budget. The budgets then bound the unretired window, which the guest keeps
// small (it retires whenever its own reserve fails).
//
// Why this is safe. A number at or below the floor is never resolved from its
// entry again:
//   - Resolve answers RETIRED_ID before loading any entry, so a QUERY of a
//     retired number cannot see absence as UNKNOWN (no no-effect is invented);
//   - Reserve answers Stale and Allocate refuses a number that is not
//     High+1, so a retired number is never admitted or allocated again;
//   - Cancel answers the RETIRED_ID state and ResolveGap Stale for it (both
//     check the floor first), never Permission or a new proof;
//   - a Retire replay returns its protected receipt before reading entries;
//     RetireOriginal's owner check passes absent numbers (it did already);
//   - a READDIR_CLEANUP replay of a recycled READDIR returns its original
//     receipt, which recycling keeps (readdir-receipts, a few hundred bytes);
//   - VerifyRetirementReceipt accepts an absent number at or below the floor
//     only together with the exact protected receipt of that request, which
//     Retire wrote after checking every covered number's result or gap hash;
//   - terminalEmpty requires every remaining entry and earmark to be at or
//     below the floor, which recycled numbers trivially satisfy.
//
// One entry is kept: the session's snapshot owner (the bridge bucket's
// "/owner"), whose BridgeState is the protected bridge snapshot that
// READDIR_CLEANUP rewrites and Restore returns. It is recycled by a later
// Retire once another claim owns the snapshot. Principals, retirement
// receipts, the operations/allocations UUID sets and the processes bucket
// are kept: they are small and keep their non-reuse guarantees.

// earmarkCreditBucket records the credits an unbound earmark reserved
// (EarmarkNumber), so recycling returns exactly what was reserved. A closed
// earmark created without credits (CloseEarmark of a number that was never
// earmarked here) has no record and returns nothing.
const earmarkCreditBucket = "earmark-credits"

type earmarkCredit struct{ Retained, Source uint64 }

// recycleRetired recycles every number of claim's family from Recycled+1 to
// the (already advanced) Floor, except the session's snapshot owner, and
// moves Recycled to the last number before the first one it kept.
func (p *Provider) recycleRetired(tx *bolt.Tx, scope guestbridge.Context, claim guestbridge.Claim, ps *principal) error {
	owner := p.bucket(tx, "bridge").Get([]byte(scope.Session + "/owner"))
	recycled, contiguous := ps.Recycled, true
	for n := ps.Recycled + 1; n <= ps.Floor; n++ {
		claim.Number = n
		key := claimKey(claim)
		if owner != nil && bytes.Equal(key, owner) {
			contiguous = false
			continue
		}
		if err := p.recycleNumber(tx, key); err != nil {
			return err
		}
		if contiguous {
			recycled = n
		}
	}
	ps.Recycled = recycled
	return nil
}

// recycleNumber deletes one retired number's entry and earmark and returns
// its credits: the entry's when it has one, else the earmark's record.
func (p *Provider) recycleNumber(tx *bolt.Tx, key []byte) error {
	var retained, source uint64
	credits := p.bucket(tx, earmarkCreditBucket)
	if raw := p.bucket(tx, "claims").Get(key); raw != nil {
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		retained, source = e.RetainedCredit, e.SourceCredit
		if err := p.keepReaddirReceipt(tx, key, e); err != nil {
			return err
		}
		if err := p.bucket(tx, "claims").Delete(key); err != nil {
			return err
		}
	} else if raw := credits.Get(key); raw != nil {
		var c earmarkCredit
		if err := json.Unmarshal(raw, &c); err != nil {
			return err
		}
		retained, source = c.Retained, c.Source
	}
	if err := credits.Delete(key); err != nil {
		return err
	}
	if err := p.bucket(tx, "earmarks").Delete(key); err != nil {
		return err
	}
	if err := p.releaseCredit(tx, "retained-credit", retained); err != nil {
		return err
	}
	return p.releaseCredit(tx, "source-credit", source)
}

// releaseCredit returns amount to a reserveCredit counter. Returning more
// than is in use means the ledger is inconsistent, which stays Stale.
func (p *Provider) releaseCredit(tx *bolt.Tx, name string, amount uint64) error {
	if amount == 0 {
		return nil
	}
	root := tx.Bucket([]byte(ledgerVersion))
	raw := root.Get([]byte(name))
	if len(raw) != 8 {
		return fsapi.Stale
	}
	used := binary.BigEndian.Uint64(raw)
	if amount > used {
		return fsapi.Stale
	}
	var next [8]byte
	binary.BigEndian.PutUint64(next[:], used-amount)
	return root.Put([]byte(name), next[:])
}

// pruneRetiredSnapshot drops the bridge snapshot's grant tombstones that the
// retired claims released (guestbridge.PruneRetiredGrants gives the rule and
// why it is replay-safe), rewriting the owner entry and the protected
// snapshot together as CleanupReaddir does. Pruning only reclaims space: if
// the owner does not match the protected snapshot, it is skipped.
func (p *Provider) pruneRetiredSnapshot(tx *bolt.Tx, scope guestbridge.Context, claim guestbridge.Claim, floor uint64) error {
	bridge := p.bucket(tx, "bridge")
	snapshot := bridge.Get([]byte(scope.Session))
	ownerKey := bridge.Get([]byte(scope.Session + "/owner"))
	if snapshot == nil || ownerKey == nil {
		return nil
	}
	var owner entry
	ownerRaw := p.bucket(tx, "claims").Get(ownerKey)
	if ownerRaw == nil || json.Unmarshal(ownerRaw, &owner) != nil || owner.Status != "TERMINAL" || !bytes.Equal(owner.BridgeState, snapshot) {
		return nil
	}
	retired := guestbridge.ClaimNumber{Realm: claim.Realm, Subject: claim.Subject, Anchor: claim.Anchor, Nonce: claim.Nonce, Number: floor}
	updated, pruned, err := guestbridge.PruneRetiredGrants(scope, snapshot, retired)
	if err != nil || pruned == 0 {
		return nil
	}
	owner.BridgeState = updated
	if owner.SnapshotVersion, err = bridge.NextSequence(); err != nil {
		return err
	}
	if err = put(p.bucket(tx, "claims"), ownerKey, owner); err != nil {
		return err
	}
	return p.protectSnapshot(tx, owner)
}
