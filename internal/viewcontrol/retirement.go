package viewcontrol

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

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
// BLOCKED while any number in the range is not closed, or a new RETIRED
// floor, which then recycles everything the floor retires (compact).
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
		return canonical(map[string]any{"status": "BLOCKED", "floor": f.Floor, "proof_hash": nil})
	}
	state, err := c.rawState()
	if err != nil {
		return nil, err
	}
	if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), scope, "VIEW_RETIRE", raw, state); err != nil {
		return nil, err
	}
	return c.protectRetirement(scope, r, fk, receiptKey)
}

// retirementBlocked reports whether the range floor+1..through cannot be
// retired yet: it is not exactly the family's next run of terminal tickets
// with these protected terminal ids, a recall runs, or the credentials still
// hold an active lease or an open reservation. A DIRTY_RESERVE with
// protected BLOCKED history stays until CANCEL closed its promised number.
func (c *Controller) retirementBlocked(scope guestbridge.Context, r retirement, f family) (bool, error) {
	if r.Through > f.High || r.Through-f.Floor > 1024 || uint64(len(r.Hashes)) != r.Through-f.Floor || c.state.Recall != nil {
		return true, nil
	}
	for _, lease := range c.state.Leases {
		if lease.Active && lease.Lease.Scope == scope && lease.Lease.CredentialHash == digest(r.Credentials) {
			return true, nil
		}
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
