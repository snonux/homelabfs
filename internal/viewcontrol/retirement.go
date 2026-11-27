package viewcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// ErrRetirementHeld is what Facts.CheckPhysical returns for a VIEW_RETIRE
// while an active physical or dirty owner remains (the runtime's
// factsLedger.checkRetire). retire answers it BLOCKED, like its own
// retirementBlocked clauses: nothing changes and the guest offers the same
// range again later (hfs_view_retire records view->retire_blocked, re4).
//
// It wraps fsapi.WouldBlock, which this refusal was until task km4. The
// transport retries WouldBlock in its pending ring and never answers the
// cell (guestbridge recordExecuteFailure), and the holder of the other lease
// usually outlives the guest's 30 s exchange: in zq3-r22 (R22-1) m0's
// cleanup worker sent access child 1's VIEW_RETIRE while child 2's and the
// creating process's observation leases were active, got FETCH AGAIN until
// its deadline (-110), blocked the session, and the CREATE waiting for
// session->retirement behind it failed ENOSPC after 33 s.
//
// Since gn4 (zq3-r24 R24-1) a lease refused there only the retirement of
// its own credentials' families, and since hp4 (zq3-r25 R25-1) only a range
// holding a GRANTED acquisition of a lease that is still active
// (GrantedLeaseID, as retirementBlocked); an open reservation still refuses
// domain-wide.
var ErrRetirementHeld = fmt.Errorf("%w: retirement refused while an active owner remains", fsapi.WouldBlock)

// CredentialDigest is the credential hash a lease carries
// (ViewLease.CredentialHash) for credentials c, as retirementBlocked
// compares it. The runtime's physical retirement check uses it (gn4) to
// find the retired family's tickets (hp4).
func CredentialDigest(c guestbridge.Credentials) string {
	return digest(c)
}

// GrantedLeaseID is the lease a ticket's current reply hands its holder: the
// lease ID of a GRANTED VIEW_ACQUIRE or VIEW_RENEW, else "" (an answered
// VIEW_RELEASE, a BLOCKED or cancelled acquisition, a DIRTY_* ticket). Only
// such a ticket can still be used by a holder of that lease, and only while
// the lease is active (hp4, retirementBlocked); the runtime's physical
// retirement check applies the same rule.
func GrantedLeaseID(current json.RawMessage) string {
	var reply struct {
		Status string
		Lease  *struct {
			ID string `json:"id"`
		}
	}
	if json.Unmarshal(current, &reply) != nil || reply.Status != "GRANTED" || reply.Lease == nil {
		return ""
	}
	return reply.Lease.ID
}

type retirement struct {
	Anchor, Nonce string
	Credentials   guestbridge.Credentials
	Through       uint64
	Hashes        []string `json:"result_hashes"`
}

// retirementKey is the Retirements key of one exact VIEW_RETIRE request.
func retirementKey(scope guestbridge.Context, r retirement) string {
	return digest(struct {
		Scope   guestbridge.Context
		Request retirement
	}{scope, r})
}

// retire answers one VIEW_RETIRE: the protected receipt of an exact replay,
// BLOCKED while any number in the range is not closed or the physical side
// refuses with ErrRetirementHeld (km4), or a new RETIRED floor, which then
// recycles everything the floor retires (compact). A BLOCKED answer is not
// stored: it is exactly retirementReply's fallback, so CheckResponse
// accepts it.
//
// Only the family's latest receipt is kept (family.Receipt). The guest
// builds a family's next VIEW_RETIRE only from the previous one's RETIRED
// floor (hfs_view_retire, patch 0004), so once a newer receipt exists it
// never sends the older request again. A replay of a range at or below the
// floor whose receipt was recycled is refused as Stale: retired numbers are
// never retired again, and the floor alone cannot rebuild the old reply's
// proof binding.
func (c *Controller) retire(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	var r retirement
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	_, fk := ticketKeys(scope, guestbridge.Ticket{Anchor: r.Anchor, Nonce: r.Nonce, Credentials: r.Credentials})
	f := c.state.Families[fk]
	receiptKey := retirementKey(scope, r)
	if receipt := c.state.Retirements[receiptKey]; receipt != nil {
		return receipt, nil
	}
	if r.Through < f.Floor || (r.Through == f.Floor && len(r.Hashes) > 0) {
		return nil, fsapi.Stale
	}
	blocked, err := c.retirementBlocked(scope, r, f)
	if err != nil {
		return nil, err
	}
	if blocked {
		return blockedRetirement(f)
	}
	state, err := c.rawState()
	if err != nil {
		return nil, err
	}
	if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), scope, "VIEW_RETIRE", raw, state); err != nil {
		if errors.Is(err, ErrRetirementHeld) {
			return blockedRetirement(f)
		}
		return nil, err
	}
	return c.protectRetirement(scope, r, fk, receiptKey)
}

// blockedRetirement is the BLOCKED answer at family f's unchanged floor.
func blockedRetirement(f family) (json.RawMessage, error) {
	return canonical(map[string]any{"status": "BLOCKED", "floor": f.Floor, "proof_hash": nil})
}

// retirementBlocked reports whether the range floor+1..through cannot be
// retired yet: it is not exactly the family's next run of terminal tickets
// with these protected terminal ids, a recall runs, one of its tickets is a
// GRANTED acquisition of a lease that is still active, or the
// credentials hold an open reservation. A DIRTY_RESERVE with protected
// BLOCKED history stays until CANCEL closed its promised number.
//
// The recall and reservation clauses date from the first ledger (k04) and
// are kept on purpose (reviewed in task qe4 after zq3-r16 R16-1):
//
//   - No retirement while a recall runs. Retiring runs compact(), which
//     drops lease rows, lineage revisions, source cuts and recall-history
//     UUIDs. Admissions already refuse to prune cuts during a recall
//     (pruneSources), and compacting mid-recall has never been analysed or
//     covered by recall, crash-replay and terminal-seal tests.
//   - No retirement while the credentials hold an open reservation.
//
// The lease clause is the host's only guard that the guest retires only
// tickets it no longer references: a GRANTED acquisition ticket of an active
// or shared lease must not be recycled while a holder may still use it. It
// was coarse until hp4: any active lease of the domain (k04), then of the
// same credentials (gn4, zq3-r24 R24-1). In zq3-r25 (R25-1) that still
// refused every VIEW_RETIRE of the busy T1 process: the guest's cleanup
// worker releases an observation's lease asynchronously, often only after
// the process's next observation has acquired its own (8 of T1's last 13
// acquisitions shared the previous, still active lease), so a lease of T1's
// credentials was active whenever its VIEW_RETIREs arrived, and 13 answered
// VIEW_RELEASE tickets (about 25.5 MiB each in the guest) filled m0's
// 512 MiB control lane. Since hp4 the clause is
// per ticket, as qe4 foresaw (with the guest's re4 re-send of a BLOCKED
// VIEW_RETIRE, a matching facts check and tests): only a GRANTED ticket
// whose lease is still active (GrantedLeaseID) refuses. A shared lease stays
// active until its last holder released it, so every holder's acquisition
// waits for that. An answered VIEW_RELEASE names no lease, and an inactive
// lease is never used again, so neither refuses; an inactive lease never
// refused before hp4 either (only Active counted), so the rule only got
// narrower. compact() keeps any lease row an unretired ticket still names,
// and never an active one. The physical side (controlledruntime
// factsLedger.checkRetire) applies the same per-ticket rule; since km4 its
// refusal (ErrRetirementHeld) is answered BLOCKED too.
func (c *Controller) retirementBlocked(scope guestbridge.Context, r retirement, f family) (bool, error) {
	if r.Through > f.High || r.Through-f.Floor > 1024 || uint64(len(r.Hashes)) != r.Through-f.Floor || c.state.Recall != nil {
		return true, nil
	}
	for _, reservation := range c.state.Reservations {
		if reservation.Status != "CLOSED" && reservation.Reservation.Scope == scope && equal(reservation.Reservation.Credentials, r.Credentials) {
			return true, nil
		}
	}
	ticket := guestbridge.Ticket{Anchor: r.Anchor, Nonce: r.Nonce, Credentials: r.Credentials}
	for n := f.Floor + 1; n <= r.Through; n++ {
		ticket.Number = n
		key, _ := ticketKeys(scope, ticket)
		t, ok := c.state.Tickets[key]
		if !ok || !terminal(t.Current) || (t.Operation == "DIRTY_RESERVE" && status(t.Current) == "BLOCKED") {
			return true, nil
		}
		var reply struct{ ID string }
		if err := json.Unmarshal(t.Current, &reply); err != nil {
			return false, err
		}
		if reply.ID != r.Hashes[n-f.Floor-1] {
			return true, nil
		}
		if id := GrantedLeaseID(t.Current); id != "" {
			if l, ok := c.state.Leases[id]; ok && l.Active {
				return true, nil
			}
		}
		if n == ^uint64(0) {
			break
		}
	}
	return false, nil
}

// protectRetirement advances the family floor, replaces the family's latest
// receipt, recycles what the new floor retired and saves all of it in one
// protected write. A crash before the write keeps the old floor, receipt and
// rows, and a replay of this exact request then retires them again.
func (c *Controller) protectRetirement(scope guestbridge.Context, r retirement, fk, receiptKey string) (json.RawMessage, error) {
	f := c.state.Families[fk]
	f.Floor = r.Through
	reply, err := canonical(map[string]any{"status": "RETIRED", "floor": f.Floor, "proof_hash": digest(struct {
		Enrollment string
		Scope      guestbridge.Context
		Retirement retirement
	}{c.enrollmentHash, scope, r})})
	if err != nil {
		return nil, err
	}
	if f.Receipt != "" {
		delete(c.state.Retirements, f.Receipt)
	}
	f.Receipt = receiptKey
	c.state.Families[fk] = f
	c.state.Retirements[receiptKey] = reply
	c.compact()
	if err = c.save(); err != nil {
		return nil, err
	}
	return c.retirementReply(scope, r), nil
}

// retirementReply is the reply CheckResponse expects for a VIEW_RETIRE: its
// protected receipt, or BLOCKED at the current floor when none is kept.
func (c *Controller) retirementReply(scope guestbridge.Context, r retirement) json.RawMessage {
	_, key := ticketKeys(scope, guestbridge.Ticket{Anchor: r.Anchor, Nonce: r.Nonce, Credentials: r.Credentials})
	f := c.state.Families[key]
	if receipt := c.state.Retirements[retirementKey(scope, r)]; receipt != nil {
		return receipt
	}
	raw, _ := canonical(map[string]any{"status": "BLOCKED", "floor": f.Floor, "proof_hash": nil})
	return raw
}

func terminal(raw json.RawMessage) bool {
	switch status(raw) {
	case "GRANTED", "FROZEN", "RELEASED", "CANCELLED", "BLOCKED":
		return true
	}
	return false
}

// ticketReferences collects the leases and reservations that unretired
// tickets' requests and replies still name, so a replay can resolve them.
func (c *Controller) ticketReferences() (leases, reservations map[string]bool) {
	leases, reservations = map[string]bool{}, map[string]bool{}
	for _, t := range c.state.Tickets {
		if c.retired(t) {
			continue
		}
		var original request
		if json.Unmarshal(t.Payload, &original) == nil {
			if original.Lease.ID != "" {
				leases[original.Lease.ID] = true
			}
			if original.Reservation.ID != "" {
				reservations[original.Reservation.ID] = true
			}
		}
		for _, raw := range []json.RawMessage{t.Initial, t.Current} {
			var response struct {
				Lease       *guestbridge.ViewLease
				Reservation *guestbridge.DirtyReservation
				Entry       *struct{ Reservation string }
			}
			if json.Unmarshal(raw, &response) != nil {
				continue
			}
			if response.Lease != nil {
				leases[response.Lease.ID] = true
			}
			if response.Reservation != nil {
				reservations[response.Reservation.ID] = true
			}
			if response.Entry != nil {
				reservations[response.Entry.Reservation] = true
			}
		}
	}
	return leases, reservations
}

// dropUnreferencedLeases deletes closed reservations and inactive leases that
// no unretired ticket or retained reservation names.
func (c *Controller) dropUnreferencedLeases() {
	leases, reservations := c.ticketReferences()
	for id, r := range c.state.Reservations {
		if r.Status == "CLOSED" && !reservations[id] {
			delete(c.state.Reservations, id)
		} else {
			leases[r.Reservation.LeaseID] = true
		}
	}
	for id, l := range c.state.Leases {
		if !l.Active && !leases[id] {
			delete(c.state.Leases, id)
		}
	}
}
