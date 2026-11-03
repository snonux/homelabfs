package viewcontrol

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

type retirement struct {
	Anchor, Nonce string
	Credentials   guestbridge.Credentials
	Through       uint64
	Hashes        []string `json:"result_hashes"`
}

func (c *Controller) retire(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	var r retirement
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	ticket := guestbridge.Ticket{Anchor: r.Anchor, Nonce: r.Nonce, Credentials: r.Credentials}
	_, fk := ticketKeys(scope, ticket)
	f := c.state.Families[fk]
	receiptKey := digest(struct {
		Scope   guestbridge.Context
		Request retirement
	}{scope, r})
	if receipt := c.state.Retirements[receiptKey]; receipt != nil {
		return receipt, nil
	}
	blocked := func() (json.RawMessage, error) {
		return canonical(map[string]any{"status": "BLOCKED", "floor": f.Floor, "proof_hash": nil})
	}
	if r.Through > f.High || r.Through-f.Floor > 1024 || uint64(len(r.Hashes)) != r.Through-f.Floor {
		return blocked()
	}
	if c.state.Recall != nil {
		return blocked()
	}
	for _, lease := range c.state.Leases {
		if lease.Active && lease.Lease.Scope == scope && lease.Lease.CredentialHash == digest(r.Credentials) {
			return blocked()
		}
	}
	for _, reservation := range c.state.Reservations {
		if reservation.Status != "CLOSED" && reservation.Reservation.Scope == scope && equal(reservation.Reservation.Credentials, r.Credentials) {
			return blocked()
		}
	}
	for n := f.Floor + 1; n <= r.Through; n++ {
		ticket.Number = n
		key, _ := ticketKeys(scope, ticket)
		t, ok := c.state.Tickets[key]
		if ok && t.Operation == "DIRTY_RESERVE" && status(t.Current) == "BLOCKED" {
			return blocked()
		}
		if !ok || !terminal(t.Current) {
			return blocked()
		}
		var reply struct{ ID string }
		if err := json.Unmarshal(t.Current, &reply); err != nil {
			return nil, err
		}
		if reply.ID != r.Hashes[n-f.Floor-1] {
			return blocked()
		}
		if n == ^uint64(0) {
			break
		}
	}
	state, err := c.rawState()
	if err != nil {
		return nil, err
	}
	if err = c.config.Facts.CheckPhysical(ctx, scope, "VIEW_RETIRE", raw, state); err != nil {
		return nil, err
	}
	f.Floor = r.Through
	c.state.Families[fk] = f
	reply, _ := canonical(map[string]any{"status": "RETIRED", "floor": f.Floor, "proof_hash": digest(struct {
		Enrollment string
		Scope      guestbridge.Context
		Retirement retirement
	}{c.enrollmentHash, scope, r})})
	c.state.Retirements[receiptKey] = reply
	c.compact()
	if err = c.save(); err != nil {
		return nil, err
	}
	return c.retirementReply(scope, r), nil
}

func (c *Controller) retirementReply(scope guestbridge.Context, r retirement) json.RawMessage {
	_, key := ticketKeys(scope, guestbridge.Ticket{Anchor: r.Anchor, Nonce: r.Nonce, Credentials: r.Credentials})
	f := c.state.Families[key]
	receiptKey := digest(struct {
		Scope   guestbridge.Context
		Request retirement
	}{scope, r})
	if receipt := c.state.Retirements[receiptKey]; receipt != nil {
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

func (c *Controller) compact() {
	// Tombstones retain the original operation/hash and complete principal key.
	// Only an already protected floor permits discarding full request/reply bytes.
	for key, t := range c.state.Tickets {
		if c.retired(t) {
			t.Payload = nil
			t.Initial = nil
			t.Current = nil
			t.Cancelled = nil
			t.DeclinedGap = nil
			t.GapClosureReply = nil
			c.state.Tickets[key] = t
		}
	}
	leases := map[string]bool{}
	reservations := map[string]bool{}
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
	targets := map[string]bool{}
	for _, l := range c.state.Leases {
		targets[digest(l.Lease.Target)] = true
	}
	if c.state.Recall != nil {
		for _, raw := range c.state.Recall.Events {
			var e struct{ Target *guestbridge.ViewTarget }
			if json.Unmarshal(raw, &e) == nil && e.Target != nil {
				targets[digest(*e.Target)] = true
			}
		}
	}
	for key, source := range c.state.Sources {
		pinned := false
		for _, enrollment := range c.config.Authorization.Enrollment.Domains {
			if targets[digest(c.target(enrollment.Scope, source))] {
				pinned = true
				break
			}
		}
		if !pinned {
			delete(c.state.Sources, key)
		}
	}
}
