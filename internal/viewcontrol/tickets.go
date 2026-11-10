package viewcontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type request struct {
	Request        guestbridge.Ticket      `json:"request"`
	Domain         guestbridge.Capability  `json:"domain"`
	Access         guestbridge.ViewAccess  `json:"access"`
	Mode           string                  `json:"mode"`
	Target         *guestbridge.ViewTarget `json:"target"`
	Lease          guestbridge.ViewLease   `json:"lease"`
	Description    guestbridge.Capability  `json:"description"`
	Offset, Length uint64
	Number         guestbridge.ClaimNumber      `json:"claim_number"`
	Reservation    guestbridge.DirtyReservation `json:"reservation"`
	Recall, Data   string
	Claim          guestbridge.Claim
}

// ExchangeView protects every admission and its immutable initial response.
// Queries observe separately protected current outcomes; cancellation has its
// own stable reply and never rewrites the original admission lineage.
func (c *Controller) ExchangeView(ctx context.Context, scope guestbridge.Context, op string, raw json.RawMessage) (json.RawMessage, error) {
	if err := c.CheckRequest(ctx, scope, op, raw); err != nil {
		return nil, err
	}
	protocol, ok := c.scopeProtocol(scope)
	if !ok {
		return nil, fsapi.Permission
	}
	envelope, err := canonical(guestbridge.Command{Cell: 1, Context: scope, Protocol: &protocol, Op: op, Payload: raw})
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, uint16(protocol.BridgeMinor)); err != nil {
		return nil, err
	}
	if !c.mu.TryLock() {
		return nil, fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	if _, sealed := c.state.Terminal[scope.Session]; sealed {
		return nil, fsapi.Permission
	}
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return nil, fsapi.Unavailable
	}
	if op == "VIEW_RETIRE" {
		var req retirement
		if err = json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		if !c.validFamily(scope, req.Anchor, req.Nonce, "") {
			return nil, fsapi.Permission
		}
		return c.retire(ctx, scope, raw)
	}
	var r request
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if !c.validFamily(scope, r.Request.Anchor, r.Request.Nonce, op) {
		return nil, fsapi.Permission
	}
	key, familyKey := ticketKeys(scope, r.Request)
	f := c.state.Families[familyKey]
	saved, exists := c.state.Tickets[key]
	if r.Request.Number <= f.Floor {
		if !exists {
			return nil, fsapi.Stale
		}
		return c.reply(saved, "RETIRED_ID", nil, nil, nil)
	}
	if exists {
		if op != "VIEW_QUERY" && op != "VIEW_CANCEL" && (saved.Operation != op || !bytes.Equal(saved.Payload, raw)) {
			return nil, fsapi.Stale
		}
		if saved.DeclinedGap != nil && status(saved.Current) != "CANCELLED" {
			if err = c.finishDeclinedGap(ctx, key, saved); err != nil {
				return nil, err
			}
			saved = c.state.Tickets[key]
		}
		for _, resource := range c.state.Reservations {
			if resource.TicketKey == key && resource.Status == "CANCELLING" {
				if err = c.closeReservation(ctx, resource); err != nil {
					return nil, err
				}
				saved = c.state.Tickets[key]
			}
		}
	}
	if op == "VIEW_QUERY" || op == "VIEW_CANCEL" {
		if !exists {
			return nil, fsapi.Permission
		}
		if op == "VIEW_QUERY" {
			var live atomic.Bool
			live.Store(true)
			defer live.Store(false)
			ctx = c.releaseQueryContext(ctx, scope, raw, r.Request, saved, &live)
		}
		if absent(saved.Initial) {
			var original request
			if err = json.Unmarshal(saved.Payload, &original); err != nil {
				return nil, err
			}
			reply, err := c.admitTicket(ctx, scope, saved.Operation, original, saved)
			if err != nil {
				return nil, err
			}
			saved = c.state.Tickets[key]
			saved.Initial = reply
			saved.Current = reply
			c.state.Tickets[key] = saved
			if err = c.save(); err != nil {
				return nil, err
			}
		}
		if op == "VIEW_CANCEL" {
			return c.cancel(ctx, key, saved)
		}
		if status(saved.Current) == "PENDING" {
			var original request
			if err = json.Unmarshal(saved.Payload, &original); err != nil {
				return nil, err
			}
			outcome, err := c.admitTicket(ctx, scope, saved.Operation, original, saved)
			if err != nil {
				return nil, err
			}
			saved = c.state.Tickets[key]
			saved.Current = outcome
			c.state.Tickets[key] = saved
			if err = c.save(); err != nil {
				return nil, err
			}
		}
		return bytes.Clone(saved.Current), nil
	}
	if exists {
		if saved.Operation != op || !bytes.Equal(saved.Payload, raw) {
			return nil, fsapi.Stale
		}
		if absent(saved.Initial) {
			reply, err := c.admitTicket(ctx, scope, op, r, saved)
			if err != nil {
				return nil, err
			}
			saved = c.state.Tickets[key]
			saved.Initial = reply
			saved.Current = reply
			c.state.Tickets[key] = saved
			if err = c.save(); err != nil {
				return nil, err
			}
			return reply, nil
		}
		return bytes.Clone(saved.Initial), nil
	}
	if f.High == ^uint64(0) || r.Request.Number != f.High+1 {
		return nil, fsapi.Stale
	}
	if err = c.ticketCapacity(op, r); err != nil {
		return nil, err
	}
	saved = ticketState{PayloadHash: digest(map[string]any{"op": op, "payload": raw}), Scope: scope, Request: r.Request, Operation: op, Payload: bytes.Clone(raw)}
	f.High = r.Request.Number
	c.state.Families[familyKey] = f
	c.state.Identities++
	c.state.Tickets[key] = saved
	outcome, err := c.admitTicket(ctx, scope, op, r, saved)
	if err != nil {
		// No failure is represented as no effect. Preserve the allocated ticket,
		// including cross-ledger partial reservations, for same-key reconciliation.
		if saveErr := c.save(); saveErr != nil {
			return nil, saveErr
		}
		return nil, err
	}
	saved = c.state.Tickets[key] // freeze may have updated its durable staging data.
	saved.Initial = outcome
	saved.Current = outcome
	c.state.Tickets[key] = saved
	if err = c.save(); err != nil {
		return nil, err
	}
	return bytes.Clone(outcome), nil
}

// CheckResponse verifies exact protected lineage without trusting returned hashes.
func (c *Controller) CheckResponse(ctx context.Context, scope guestbridge.Context, op string, request, response json.RawMessage) error {
	if err := c.CheckRequest(ctx, scope, op, request); err != nil {
		return err
	}
	if !c.mu.TryLock() {
		return fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	if op == "VIEW_RETIRE" {
		var r retirement
		if err := json.Unmarshal(request, &r); err != nil {
			return err
		}
		expected := c.retirementReply(scope, r)
		if !bytes.Equal(expected, response) {
			return fsapi.Stale
		}
		return nil
	}
	var r struct {
		Request guestbridge.Ticket `json:"request"`
	}
	if err := json.Unmarshal(request, &r); err != nil {
		return err
	}
	key, familyKey := ticketKeys(scope, r.Request)
	saved, ok := c.state.Tickets[key]
	if !ok {
		return fsapi.Permission
	}
	if r.Request.Number <= c.state.Families[familyKey].Floor {
		expected, err := c.reply(saved, "RETIRED_ID", nil, nil, nil)
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, response) {
			return fsapi.Stale
		}
		return nil
	}
	var expected json.RawMessage
	switch op {
	case "VIEW_QUERY":
		expected = saved.Current
	case "VIEW_CANCEL":
		expected = saved.Cancelled
	default:
		if saved.Operation != op || !bytes.Equal(saved.Payload, request) {
			return fsapi.Stale
		}
		expected = saved.Initial
	}
	if absent(expected) || !bytes.Equal(expected, response) {
		return fsapi.Stale
	}
	return nil
}

func (c *Controller) admitTicket(ctx context.Context, scope guestbridge.Context, op string, r request, t ticketState) (json.RawMessage, error) {
	switch op {
	case "VIEW_ACQUIRE":
		return c.acquire(ctx, scope, r, t)
	case "VIEW_RENEW", "VIEW_RELEASE":
		return c.changeLease(ctx, scope, op, r, t)
	case "DIRTY_RESERVE":
		return c.reserve(ctx, scope, r, t)
	case "DIRTY_FREEZE":
		return c.freeze(ctx, scope, r, t)
	default:
		return nil, fsapi.Unsupported
	}
}

func (c *Controller) acquire(ctx context.Context, scope guestbridge.Context, r request, t ticketState) (json.RawMessage, error) {
	d := c.state.Domains[scope.Session]
	if d.Domain.Token != "" && !equal(d.Domain, r.Domain) {
		return nil, fsapi.Stale
	}
	if c.state.Recall != nil {
		return c.reply(t, "PENDING", nil, nil, nil)
	}
	d.Domain = r.Domain
	c.state.Domains[scope.Session] = d
	if err := c.viewReady(ctx); err != nil {
		return c.reply(t, "PENDING", nil, nil, nil)
	}
	for _, l := range c.state.Leases {
		if l.Active && accessConflict(l.Lease.Access, l.Lease.Mode, r.Access, r.Mode) {
			if err := c.startRecall(ctx); err != nil {
				return nil, err
			}
			return c.reply(t, "PENDING", nil, nil, nil)
		}
	}
	source, err := c.capture(ctx)
	if err != nil {
		return nil, err
	}
	target := c.target(scope, source)
	if r.Target != nil && !equal(*r.Target, target) {
		if t.ExpectedMismatch {
			return c.reply(t, "BLOCKED", nil, nil, nil)
		}
		key, _ := ticketKeys(scope, t.Request)
		t.ExpectedMismatch = true
		c.state.Tickets[key] = t
		if err = c.startRecall(ctx); err != nil {
			return nil, err
		}
		return c.reply(t, "PENDING", nil, nil, nil)
	}
	if err = c.remember(source); err != nil {
		return c.reply(t, "BLOCKED", nil, nil, nil)
	}
	lease := guestbridge.ViewLease{Scope: scope, Domain: r.Domain, Access: r.Access, CredentialHash: digest(r.Request.Credentials), Mode: r.Mode, Target: target, Fence: c.fence(scope.Session), Revision: 1}
	for _, existing := range c.state.Leases {
		if existing.Active && sameLineage(existing.Lease, lease) {
			copyLease := existing.Lease
			return c.reply(t, "GRANTED", &copyLease, nil, nil)
		}
	}
	if len(c.state.Leases) >= c.config.Authorization.Enrollment.Bounds.Identities || !c.controlCapacity(1) || !c.identityCapacity(1) {
		return c.reply(t, "BLOCKED", nil, nil, nil)
	}
	lineage := lineageID(lease)
	high := c.state.Revisions[lineage]
	if high == ^uint64(0) {
		return c.reply(t, "BLOCKED", nil, nil, nil)
	}
	lease.Revision = high + 1
	c.state.Revisions[lineage] = lease.Revision
	lease.ID = objectID(lease)
	c.state.Leases[lease.ID] = leaseState{Lease: lease, Active: true, ReleaseCredit: true}
	return c.reply(t, "GRANTED", &lease, nil, nil)
}

func (c *Controller) changeLease(ctx context.Context, scope guestbridge.Context, op string, r request, t ticketState) (json.RawMessage, error) {
	old, ok := c.state.Leases[r.Lease.ID]
	if !ok || !equal(old.Lease, r.Lease) || old.Lease.Scope != scope || old.Lease.CredentialHash != digest(r.Request.Credentials) {
		return nil, fsapi.Permission
	}
	if op == "VIEW_RENEW" {
		if !old.Active || c.state.Recall != nil {
			return c.reply(t, "BLOCKED", nil, nil, nil)
		}
		if old.Lease.Revision == ^uint64(0) || len(c.state.Leases) >= c.config.Authorization.Enrollment.Bounds.Identities || !c.controlCapacity(1) || !c.identityCapacity(1) {
			return c.reply(t, "BLOCKED", nil, nil, nil)
		}
		next := old.Lease
		next.Revision++
		c.state.Revisions[lineageID(next)] = next.Revision
		next.ID = objectID(next)
		old.Active = false
		c.state.Leases[r.Lease.ID] = old
		c.state.Leases[next.ID] = leaseState{Lease: next, Active: true, ReleaseCredit: true}
		return c.reply(t, "GRANTED", &next, nil, nil)
	}
	if c.state.Recall != nil {
		return c.reply(t, "PENDING", nil, nil, nil)
	}
	// A clean ACTIVE lease may still own readable PTEs. Its preowned release
	// must start the same physical recall as dirty pressure; CheckPhysical
	// cannot certify a live mapping as already closed. Include active renewed
	// siblings so an old immutable lineage release cannot wait without progress.
	for _, lease := range c.state.Leases {
		if lease.Active && sameLineage(lease.Lease, r.Lease) {
			if err := c.startRecall(ctx); err != nil {
				return nil, err
			}
			return c.reply(t, "PENDING", nil, nil, nil)
		}
	}
	for _, reservation := range c.state.Reservations {
		if sameLineage(c.state.Leases[reservation.Reservation.LeaseID].Lease, r.Lease) && reservation.Status != "CLOSED" {
			if err := c.startRecall(ctx); err != nil {
				return nil, err
			}
			return c.reply(t, "PENDING", nil, nil, nil)
		}
	}
	ledger, err := c.rawState()
	if err != nil {
		return nil, err
	}
	if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), scope, op, t.Payload, ledger); err != nil {
		return nil, err
	}
	old.Active = false
	old.ReleaseCredit = false
	c.state.Leases[r.Lease.ID] = old
	return c.reply(t, "RELEASED", nil, nil, nil)
}

func (c *Controller) reserve(ctx context.Context, scope guestbridge.Context, r request, t ticketState) (json.RawMessage, error) {
	key, _ := ticketKeys(scope, t.Request)
	for _, existing := range c.state.Reservations {
		if existing.TicketKey == key {
			return c.finishReserve(ctx, existing, t)
		}
	}
	lease, ok := c.state.Leases[r.Lease.ID]
	if !ok || !lease.Active || !equal(lease.Lease, r.Lease) || c.state.Recall != nil {
		return c.reply(t, "BLOCKED", nil, nil, nil)
	}
	for _, existing := range c.state.Reservations {
		ancestor := c.state.Leases[existing.Reservation.LeaseID]
		if existing.Status != "CLOSED" && sameLineage(ancestor.Lease, r.Lease) {
			return c.reply(t, "BLOCKED", nil, nil, nil)
		}
	}
	if len(c.state.Reservations) >= c.config.Authorization.Enrollment.Bounds.Reservations || !c.controlCapacity(1) || !c.identityCapacity(1) || !c.manifestCapacity(r) || !c.sourceCapacity(ctx, 1) {
		return c.reply(t, "BLOCKED", nil, nil, nil)
	}
	if err := c.reserveSourceBytes(ctx, r); err != nil {
		if errors.Is(err, fsapi.NoSpace) {
			return c.reply(t, "BLOCKED", nil, nil, nil)
		}
		return nil, err
	}
	provenance, err := c.newUUID()
	if err != nil {
		return nil, err
	}
	reservation := guestbridge.DirtyReservation{Scope: scope, LeaseID: r.Lease.ID, Description: r.Description, Credentials: r.Request.Credentials, Offset: r.Offset, Length: r.Length, ClaimNumber: r.Number, Provenance: provenance, DrainCredit: hash([]byte(provenance + "reserved-control-drain"))}
	reservation.ID = objectID(reservation)
	pending := reservationState{TicketKey: key, Reservation: reservation, Status: "RESERVING"}
	c.state.Reservations[reservation.ID] = pending
	if err = c.save(); err != nil {
		return nil, err
	}
	return c.finishReserve(ctx, pending, t)
}

func (c *Controller) finishReserve(ctx context.Context, s reservationState, t ticketState) (json.RawMessage, error) {
	r := s.Reservation
	if s.Status == "RESERVING" {
		if err := c.config.Providers[r.Scope.Session].EarmarkNumber(ctx, controlledvalidation.Earmark{Scope: r.Scope, Number: r.ClaimNumber, Credentials: r.Credentials, Description: r.Description, Reservation: r.ID, Offset: r.Offset, Length: r.Length}); err != nil {
			return nil, err
		}
		if err := c.fail("earmark-protected"); err != nil {
			return nil, err
		}
		s.Status = "RESERVED"
		c.state.Reservations[r.ID] = s
	}
	if s.Status != "RESERVED" {
		return nil, fsapi.Stale
	}
	return c.reply(t, "GRANTED", nil, &r, nil)
}

func (c *Controller) freeze(ctx context.Context, scope guestbridge.Context, r request, t ticketState) (json.RawMessage, error) {
	key, _ := ticketKeys(scope, t.Request)
	s, ok := c.state.Reservations[r.Reservation.ID]
	if !ok || !equal(s.Reservation, r.Reservation) || scope != s.Reservation.Scope || c.state.Recall == nil || c.state.Recall.ID != r.Recall {
		return nil, fsapi.Stale
	}
	entry, call, err := frozen(r)
	if err != nil {
		return nil, err
	}
	if s.Call != nil {
		if s.Recall != r.Recall || !equal(*s.Call, call) || !bytes.Equal(s.Entry, entry) {
			return nil, fsapi.Stale
		}
	} else {
		if s.Status != "RESERVED" {
			return nil, fsapi.Stale
		}
		s.Call = &call
		s.Entry = entry
		s.Recall = r.Recall
		s.Status = "FROZEN_PENDING"
		c.state.Reservations[r.Reservation.ID] = s
		if err = c.save(); err != nil {
			return nil, err
		}
	}
	if err = c.config.Providers[scope.Session].BindFrozen(ctx, scope, r.Reservation.ID, call); err != nil {
		return nil, err
	}
	if err = c.fail("frozen-claim-protected"); err != nil {
		return nil, err
	}
	s.Status = "FROZEN"
	c.state.Reservations[r.Reservation.ID] = s
	outcome, err := c.reply(t, "FROZEN", nil, nil, entry)
	if err != nil {
		return nil, err
	}
	t.Initial = outcome
	t.Current = outcome
	c.state.Tickets[key] = t
	return outcome, nil
}

func (c *Controller) cancel(ctx context.Context, key string, t ticketState) (json.RawMessage, error) {
	if !absent(t.Cancelled) {
		return bytes.Clone(t.Cancelled), nil
	}
	if t.Operation == "DIRTY_RESERVE" && status(t.Current) == "BLOCKED" {
		ledger, err := c.rawState()
		if err != nil {
			return nil, err
		}
		if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), t.Scope, "VIEW_CANCEL", t.Payload, ledger); err != nil {
			return nil, err
		}
		if err = c.finishDeclinedGap(ctx, key, t); err != nil {
			return nil, err
		}
		return bytes.Clone(c.state.Tickets[key].Cancelled), nil
	}
	if status(t.Current) == "PENDING" && t.Operation == "VIEW_RELEASE" {
		return c.blockedCancel(key, t)
	}
	if status(t.Current) == "PENDING" || status(t.Current) == "BLOCKED" {
		// Admission was never a physical grant; recall obligations are retained.
		reply, err := c.reply(t, "CANCELLED", nil, nil, nil)
		if err != nil {
			return nil, err
		}
		t.Cancelled = reply
		t.Current = reply
		c.state.Tickets[key] = t
		if err = c.save(); err != nil {
			return nil, err
		}
		return reply, nil
	}
	ledger, err := c.rawState()
	if err != nil {
		return nil, err
	}
	if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), t.Scope, "VIEW_CANCEL", t.Payload, ledger); err != nil {
		return nil, err
	}
	var granted struct {
		Lease       *guestbridge.ViewLease
		Reservation *guestbridge.DirtyReservation
		Entry       json.RawMessage
	}
	if err = json.Unmarshal(t.Current, &granted); err != nil {
		return nil, err
	}
	if granted.Reservation != nil {
		s := c.state.Reservations[granted.Reservation.ID]
		if s.Call != nil || (s.Status != "RESERVED" && s.Status != "RESERVING" && s.Status != "CANCELLING" && s.Status != "CLOSED") {
			return c.blockedCancel(key, t)
		}
		if c.state.Recall != nil && (c.state.Recall.Phase != "RECALL" || !absent(c.state.Recall.Manifests[t.Scope.Session])) {
			return c.blockedCancel(key, t)
		}
		if err = c.closeReservation(ctx, s); err != nil {
			return nil, err
		}
		s = c.state.Reservations[s.Reservation.ID]

		ancestor := c.state.Leases[s.Reservation.LeaseID].Lease
		for id, l := range c.state.Leases {
			if sameLineage(l.Lease, ancestor) {
				l.Active = false
				c.state.Leases[id] = l
			}
		}
	}
	if len(granted.Entry) > 0 && string(granted.Entry) != "null" {
		return c.blockedCancel(key, t)
	}
	if granted.Lease != nil {
		for _, r := range c.state.Reservations {
			if sameLineage(c.state.Leases[r.Reservation.LeaseID].Lease, *granted.Lease) && r.Status != "CLOSED" {
				return c.blockedCancel(key, t)
			}
		}
		for id, l := range c.state.Leases {
			if sameLineage(l.Lease, *granted.Lease) {
				l.Active = false
				l.ReleaseCredit = false
				c.state.Leases[id] = l
			}
		}
	}
	reply, err := c.reply(t, "CANCELLED", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	t.Cancelled = reply
	t.Current = reply
	c.state.Tickets[key] = t
	if err = c.save(); err != nil {
		return nil, err
	}
	return reply, nil
}

func (c *Controller) reply(t ticketState, status string, lease *guestbridge.ViewLease, reservation *guestbridge.DirtyReservation, entry json.RawMessage) (json.RawMessage, error) {
	if len(entry) == 0 {
		entry = json.RawMessage("null")
	}
	response := map[string]any{"status": status, "request": t.Request, "operation": t.Operation, "request_hash": t.PayloadHash, "lease": lease, "reservation": reservation, "entry": entry}
	response["id"] = digest(response)
	return canonical(response)
}

func (c *Controller) ticketCapacity(op string, r request) error {
	b := c.config.Authorization.Enrollment.Bounds
	ordinary, control := 0, 0
	for _, t := range c.state.Tickets {
		if c.retired(t) {
			continue
		}
		if controlTicket(t.Operation) {
			control++
		} else {
			ordinary++
		}
	}
	if c.state.Identities >= b.Identities || (!controlTicket(op) && (ordinary >= b.Tickets || !c.identityCapacity(1))) {
		return fsapi.NoSpace
	}
	if !controlTicket(op) {
		return nil
	}
	if control >= b.ControlTickets {
		return fsapi.NoSpace
	}
	if op == "DIRTY_FREEZE" {
		s, ok := c.state.Reservations[r.Reservation.ID]
		if ok && s.Status == "RESERVED" && equal(s.Reservation, r.Reservation) && c.state.Recall != nil && c.state.Recall.ID == r.Recall {
			return nil
		}
	}
	if op == "VIEW_RELEASE" {
		l, ok := c.state.Leases[r.Lease.ID]
		if ok && l.ReleaseCredit && l.ReleaseTicket == "" && equal(l.Lease, r.Lease) {
			key, _ := ticketKeys(r.Lease.Scope, r.Request)
			l.ReleaseTicket = key
			c.state.Leases[r.Lease.ID] = l
			return nil
		}
	}
	if !c.controlCapacity(1) || !c.identityCapacity(1) {
		return fsapi.NoSpace
	}
	return nil
}

func controlTicket(op string) bool { return op == "DIRTY_FREEZE" || op == "VIEW_RELEASE" }

func ticketKeys(scope guestbridge.Context, ticket guestbridge.Ticket) (string, string) {
	familyKey := digest(struct {
		Scope         guestbridge.Context
		Credentials   guestbridge.Credentials
		Anchor, Nonce string
	}{scope, ticket.Credentials, ticket.Anchor, ticket.Nonce})
	return familyKey + "/" + strconv.FormatUint(ticket.Number, 10), familyKey
}

func objectID(value any) string {
	raw, _ := canonical(value)
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&object)
	delete(object, "id")
	return digest(object)
}

func status(raw json.RawMessage) string {
	var v struct{ Status string }
	_ = json.Unmarshal(raw, &v)
	return v.Status
}

func accessConflict(a guestbridge.ViewAccess, am string, b guestbridge.ViewAccess, bm string) bool {
	if am == "READ" && bm == "READ" {
		return false
	}
	if a.Kind == "DOMAIN" || b.Kind == "DOMAIN" {
		return true
	}
	object := func(a guestbridge.ViewAccess) (string, string) {
		if a.Object != nil {
			return a.Object.Home, a.Object.Allocation
		}
		if a.Description != nil {
			return a.Description.Home, a.Description.Allocation
		}
		return "", ""
	}
	ah, aa := object(a)
	bh, ba := object(b)
	if ah != bh || aa != ba {
		return false
	}
	if a.Kind != "PAGE" || b.Kind != "PAGE" {
		return true
	}
	return *a.Offset == *b.Offset
}

func frozen(r request) (json.RawMessage, guestbridge.Call, error) {
	data, err := hex.DecodeString(r.Data)
	if err != nil {
		return nil, guestbridge.Call{}, err
	}
	entry := map[string]any{"description": r.Reservation.Description, "offset": r.Reservation.Offset, "length": len(data), "payload_hash": hash(data), "reservation": r.Reservation.ID}
	entry["id"] = digest(entry)
	entry["claim"] = r.Claim
	args, err := canonical(map[string]any{"description": r.Reservation.Description, "offset": r.Reservation.Offset, "data": r.Data, "recall": r.Recall, "dirty_entry": entry["id"], "reservation": r.Reservation.ID})
	if err != nil {
		return nil, guestbridge.Call{}, err
	}
	call := guestbridge.Call{Claim: r.Claim, Credentials: r.Reservation.Credentials, Intent: guestbridge.Intent{Opcode: "DRAIN_WRITE", Args: args}}
	if call.Claim.OriginalHash != digest(call.Intent) {
		return nil, guestbridge.Call{}, fsapi.Invalid
	}
	raw, err := canonical(entry)
	return raw, call, err
}

func sameLineage(a, b guestbridge.ViewLease) bool {
	a.ID = ""
	a.Revision = 0
	b.ID = ""
	b.Revision = 0
	return equal(a, b)
}

func (c *Controller) controlCapacity(extra int) bool {
	used := extra
	for _, t := range c.state.Tickets {
		if c.retired(t) {
			continue
		}
		if controlTicket(t.Operation) {
			used++
		}
	}
	for _, l := range c.state.Leases {
		if l.ReleaseCredit && l.ReleaseTicket == "" {
			used++
		}
	}
	for _, r := range c.state.Reservations {
		if r.Status == "RESERVED" || r.Status == "RESERVING" {
			used++
		}
	}
	return used <= c.config.Authorization.Enrollment.Bounds.ControlTickets
}

func (c *Controller) manifestCapacity(request request) bool {
	// Account a complete maximum-length future entry without binding an unknown
	// OriginalHash. Only sizing uses fixed-width hash text, never a Claim record.
	size := func(description guestbridge.Capability, n guestbridge.ClaimNumber) int {
		raw, _ := canonical(map[string]any{"id": string(make([]byte, 64)), "description": description, "offset": uint64(1<<63 - 1), "length": 4096, "payload_hash": string(make([]byte, 64)), "reservation": string(make([]byte, 64)), "claim_number": n})
		return len(raw) + 256
	}
	total := size(request.Description, request.Number) + 65536
	for _, r := range c.state.Reservations {
		if r.Status != "CLOSED" {
			total += size(r.Reservation.Description, r.Reservation.ClaimNumber)
		}
	}
	return total <= guestbridge.BodyBytes
}

func (c *Controller) retired(t ticketState) bool {
	_, family := ticketKeys(t.Scope, t.Request)
	return t.Request.Number <= c.state.Families[family].Floor
}

func (c *Controller) sourceCapacity(ctx context.Context, extra int) bool {
	used := len(c.state.Sources) + extra + 2
	for _, r := range c.state.Reservations {
		if r.Status == "CLOSED" {
			continue
		}
		if r.Call != nil && c.config.Providers[r.Reservation.Scope.Session].CheckFrozenResult(ctx, r.Reservation.Scope, *r.Call) == nil {
			continue
		}
		used++
	}
	return used <= c.config.Authorization.Enrollment.Bounds.Sources
}

func absent(raw json.RawMessage) bool { return len(raw) == 0 || bytes.Equal(raw, []byte("null")) }

func (c *Controller) identityCapacity(extra int) bool {
	used := c.state.Identities + extra
	for _, l := range c.state.Leases {
		if l.ReleaseCredit && l.ReleaseTicket == "" {
			used++
		}
	}
	for _, r := range c.state.Reservations {
		if r.Status == "RESERVED" || r.Status == "RESERVING" {
			used++
		}
	}
	return used <= c.config.Authorization.Enrollment.Bounds.Identities
}

func (c *Controller) blockedCancel(key string, t ticketState) (json.RawMessage, error) {
	reply, err := c.reply(t, "BLOCKED", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	t.Cancelled = reply
	c.state.Tickets[key] = t
	if err = c.save(); err != nil {
		return nil, err
	}
	return reply, nil
}

func (c *Controller) validFamily(scope guestbridge.Context, anchor, nonce, op string) bool {
	for i, domain := range c.config.Authorization.Enrollment.Domains {
		if domain.Scope != scope {
			continue
		}
		f := c.config.Authorization.Enrollment.TicketFamilies[i]
		actual := TicketFamily{anchor, nonce}
		if op == "VIEW_QUERY" || op == "VIEW_CANCEL" || op == "" {
			return actual == f.Acquisition || actual == f.Control
		}
		if controlTicket(op) {
			return actual == f.Control
		}
		return actual == f.Acquisition
	}
	return false
}

func lineageID(lease guestbridge.ViewLease) string {
	lease.ID = ""
	lease.Revision = 0
	return digest(lease)
}
