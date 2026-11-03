package viewcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strconv"

	bolt "go.etcd.io/bbolt"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type acknowledgement struct {
	Domain        guestbridge.Capability `json:"domain"`
	Sequence      uint64                 `json:"sequence"`
	Recall, Phase string
	Authority     string `json:"authority_hash"`
	Closure       string `json:"closure_hash"`
	Inventory     string `json:"inventory_hash"`
	Session       string
	Manifest      json.RawMessage
	Target        *guestbridge.ViewTarget
}

// Exchange dispatches the retained recall protocol. Every acknowledgement is
// protected before phase advance and must independently pass physical Facts.
func (c *Controller) Exchange(ctx context.Context, scope guestbridge.Context, op string, raw json.RawMessage) (json.RawMessage, error) {
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
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return nil, fsapi.Unavailable
	}
	if op == "RESUME" {
		return canonical(map[string]any{"status": "BLOCKED", "ledger_hash": digest(c.state)})
	}
	if op == "VIEW_POLL" {
		var p struct {
			Domain guestbridge.Capability
			After  uint64 `json:"after_sequence"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		domain, ok := c.state.Domains[scope.Session]
		if !ok || !equal(domain.Domain, p.Domain) {
			return nil, fsapi.Permission
		}
		if err := c.progress(ctx); err != nil {
			return nil, err
		}
		var event json.RawMessage
		if c.state.Recall != nil {
			event = c.state.Recall.Events[scope.Session]
		}
		if len(event) == 0 {
			return canonical(map[string]any{"status": "EMPTY", "event": nil})
		}
		var e struct{ Sequence uint64 }
		if err := json.Unmarshal(event, &e); err != nil {
			return nil, err
		}
		if p.After >= e.Sequence {
			return canonical(map[string]any{"status": "EMPTY", "event": nil})
		}
		return canonical(map[string]any{"status": "EVENT", "event": event})
	}
	if op != "VIEW_ACK" {
		return nil, fsapi.Unsupported
	}
	var ack acknowledgement
	if err := json.Unmarshal(raw, &ack); err != nil {
		return nil, err
	}
	if err := c.ack(ctx, scope, ack, raw); err != nil {
		return nil, err
	}
	if err := c.save(); err != nil {
		return nil, err
	}
	if err := c.progress(ctx); err != nil {
		return nil, err
	}
	return canonical(map[string]any{"status": "ACCEPTED"})
}

// ValidateDrain checks complete immutable original call bytes, provenance and
// reserved execution credit. It never authorizes a different claim or relookup.
func (c *Controller) ValidateDrain(ctx context.Context, b guestbridge.Binding) error {
	if err := c.check(ctx, b.Context); err != nil {
		return err
	}
	var saved ledger
	if err := c.db.View(func(tx *bolt.Tx) error {
		return json.Unmarshal(tx.Bucket([]byte(version)).Get([]byte("state")), &saved)
	}); err != nil {
		return err
	}
	if saved.Recall == nil {
		return fsapi.Unavailable
	}
	for _, r := range saved.Reservations {
		if r.Call != nil && r.Status == "FROZEN" && r.Reservation.Scope == b.Context && equal(*r.Call, b.Call) && r.Recall == saved.Recall.ID {
			return nil
		}
	}
	return fsapi.Permission
}

// CheckFrozenBinding verifies the durably staged exact call before the separate
// provider may bind its earmark. FROZEN_PENDING survives either-ledger crashes.
func (c *Controller) CheckFrozenBinding(ctx context.Context, scope guestbridge.Context, reservation string, call guestbridge.Call) error {
	if err := c.check(ctx, scope); err != nil {
		return err
	}
	return c.db.View(func(tx *bolt.Tx) error {
		var state ledger
		if err := json.Unmarshal(tx.Bucket([]byte(version)).Get([]byte("state")), &state); err != nil {
			return err
		}
		s, ok := state.Reservations[reservation]
		if !ok || s.Call == nil || !equal(*s.Call, call) || s.Reservation.Scope != scope || (s.Status != "FROZEN_PENDING" && s.Status != "FROZEN") || state.Recall == nil || state.Recall.ID != s.Recall {
			return fsapi.Stale
		}
		return nil
	})
}

// CheckEarmarkClosure requires an already protected physical/source closure
// stage before the independent ordinary ledger may resolve an unsent gap.
func (c *Controller) CheckEarmarkClosure(ctx context.Context, earmark controlledvalidation.Earmark, proof string) error {
	if err := c.check(ctx, earmark.Scope); err != nil {
		return err
	}
	return c.db.View(func(tx *bolt.Tx) error {
		var state ledger
		if err := json.Unmarshal(tx.Bucket([]byte(version)).Get([]byte("state")), &state); err != nil {
			return err
		}
		s, ok := state.Reservations[earmark.Reservation]
		if !ok {
			for key, t := range state.Tickets {
				if t.DeclinedGap != nil && equal(*t.DeclinedGap, earmark) && entryID(t.GapClosureReply) == proof && t.Operation == "DIRTY_RESERVE" && status(t.Initial) == "BLOCKED" {
					for _, r := range state.Reservations {
						if r.TicketKey == key {
							return fsapi.Stale
						}
					}
					return nil
				}
			}
			return fsapi.Stale
		}
		if s.Call != nil || (s.Status != "CANCELLING" && s.Status != "CLOSED") || !equal(earmark, numberEarmark(s.Reservation)) || entryID(s.ClosureReply) != proof {
			return fsapi.Stale
		}
		return nil
	})
}

func (c *Controller) validateDrain(b guestbridge.Binding) error {
	if c.state.Recall == nil {
		return fsapi.Unavailable
	}
	for _, r := range c.state.Reservations {
		if r.Call != nil && r.Status == "FROZEN" && r.Reservation.Scope == b.Context && equal(*r.Call, b.Call) && r.Recall == c.state.Recall.ID {
			return nil
		}
	}
	return fsapi.Permission
}

func (c *Controller) startRecall(ctx context.Context) error {
	if c.state.Recall != nil {
		return nil
	}
	if c.state.Fence == ^uint64(0) || len(c.state.AckArchive)+8*len(c.config.Authorization.Enrollment.Domains) > c.config.Authorization.Enrollment.Bounds.Identities*8 {
		return fsapi.NoSpace
	}
	id, err := c.newUUID()
	if err != nil {
		return err
	}
	inventory := digest(c.state.Domains)
	c.state.Recall = &recallState{ID: id, Phase: "RECALL", Inventory: inventory, Closure: digest(struct{ Enrollment, Inventory, Recall string }{c.enrollmentHash, inventory, id}), Events: map[string]json.RawMessage{}, Acks: map[string]json.RawMessage{}, Manifests: map[string]json.RawMessage{}}
	if err = c.events(ctx, "RECALL"); err != nil {
		return err
	}
	return c.save()
}

func (c *Controller) events(ctx context.Context, phase string) error {
	recall := c.state.Recall
	source, err := c.capture(ctx)
	if err != nil {
		return err
	}
	if err = c.remember(source); err != nil {
		return err
	}
	for session, domain := range c.state.Domains {
		if domain.Sequence == ^uint64(0) {
			return fsapi.NoSpace
		}
		domain.Sequence++
		c.state.Domains[session] = domain
		var target any
		var manifest any
		if phase != "RECALL" {
			target = c.target(domain.Domain.Scope, source)
			manifest = manifestHash(recall.Manifests[session])
		}
		event := map[string]any{"sequence": domain.Sequence, "recall": recall.ID, "phase": phase, "domain": domain.Domain, "authority_hash": domain.Domain.Scope.AuthorityHash, "closure_hash": recall.Closure, "inventory_hash": recall.Inventory, "old_visibility": source.Visibility.String(), "target": target, "manifest_hash": manifest}
		raw, err := canonical(event)
		if err != nil {
			return err
		}
		recall.Events[session] = raw
	}
	return nil
}

func (c *Controller) ack(ctx context.Context, scope guestbridge.Context, ack acknowledgement, raw json.RawMessage) error {
	archiveKey := digest(scope) + "/" + ack.Recall + "/" + ack.Phase + "/" + stringNumber(ack.Sequence)
	if prior := c.state.AckArchive[archiveKey]; prior != "" {
		if prior != hash(raw) {
			return fsapi.Stale
		}
		if c.state.Recall != nil && c.state.Recall.ID == ack.Recall {
			key := scope.Session + "/" + ack.Phase + "/" + stringNumber(ack.Sequence)
			if c.state.Recall.Acks[key] == nil {
				state, err := c.rawState()
				if err != nil {
					return err
				}
				if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), scope, "VIEW_ACK", raw, state); err != nil {
					return err
				}
				c.state.Recall.Acks[key] = bytes.Clone(raw)
			}
		}
		return nil
	}
	recall := c.state.Recall
	if recall == nil || ack.Recall != recall.ID || ack.Session != scope.Session || ack.Domain.Scope != scope || ack.Authority != scope.AuthorityHash || ack.Closure != recall.Closure || ack.Inventory != recall.Inventory {
		return fsapi.Stale
	}
	event := recall.Events[scope.Session]
	var issued struct {
		Sequence uint64
		Domain   guestbridge.Capability
		Target   *guestbridge.ViewTarget
		Phase    string
	}
	if err := json.Unmarshal(event, &issued); err != nil {
		return err
	}
	if ack.Sequence != issued.Sequence || !equal(ack.Domain, issued.Domain) {
		return fsapi.Stale
	}
	key := scope.Session + "/" + ack.Phase + "/" + stringNumber(ack.Sequence)
	if old := recall.Acks[key]; old != nil {
		if !bytes.Equal(old, raw) {
			return fsapi.Stale
		}
		return nil
	}
	ledger, err := c.rawState()
	if err != nil {
		return err
	}
	if err = c.config.Facts.CheckPhysical(c.factsContext(ctx), scope, "VIEW_ACK", raw, ledger); err != nil {
		return err
	}
	switch ack.Phase {
	case "STOPPED":
		if (recall.Phase != "RECALL" && recall.Phase != "DRAIN") || issued.Phase != "RECALL" || ack.Target != nil {
			return fsapi.Stale
		}
		if err := c.closeUnadopted(ctx, scope, ack, raw); err != nil {
			return err
		}
		expected, err := c.manifest(scope)
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, ack.Manifest) {
			return fsapi.Stale
		}
		recall.Manifests[scope.Session] = bytes.Clone(ack.Manifest)
	case "INSTALLED_DRAIN":
		if (recall.Phase != "INSTALL_DRAIN" && recall.Phase != "WAIT_DRAINED") || issued.Phase != "INSTALL_DRAIN" || !equal(ack.Target, issued.Target) || string(ack.Manifest) != "null" {
			return fsapi.Stale
		}
	case "DRAINED":
		if recall.Phase != "WAIT_DRAINED" || !equal(ack.Target, issued.Target) || !bytes.Equal(ack.Manifest, recall.Manifests[scope.Session]) {
			return fsapi.Stale
		}
		if err = c.drained(ctx); err != nil {
			return err
		}
	case "INSTALLED_FINAL":
		if recall.Phase != "INSTALL_FINAL" || issued.Phase != "INSTALL_FINAL" || !equal(ack.Target, issued.Target) || string(ack.Manifest) != "null" {
			return fsapi.Stale
		}
	case "REOPENED":
		if recall.Phase != "REOPEN" || issued.Phase != "REOPEN" || !equal(ack.Target, issued.Target) || string(ack.Manifest) != "null" {
			return fsapi.Stale
		}
	default:
		return fsapi.Invalid
	}
	recall.Acks[key] = bytes.Clone(raw)
	c.state.AckArchive[archiveKey] = hash(raw)
	return nil
}

func (c *Controller) progress(ctx context.Context) error {
	r := c.state.Recall
	if r == nil {
		return nil
	}
	changed := false
	switch r.Phase {
	case "RECALL":
		if !c.allAcked("STOPPED") {
			return nil
		}
		// All physical rights are quiesced. Frozen drain provenance remains retained.
		for id, l := range c.state.Leases {
			l.Active = false
			c.state.Leases[id] = l
		}
		r.Phase = "DRAIN"
		changed = true
	case "DRAIN":
		if !c.allAcked("STOPPED") {
			return nil
		}
		if err := c.drained(ctx); err != nil {
			return nil
		}
		r.Phase = "INSTALL_DRAIN"
		if err := c.events(ctx, "INSTALL_DRAIN"); err != nil {
			return err
		}
		changed = true
	case "INSTALL_DRAIN":
		if !c.allAcked("INSTALLED_DRAIN") {
			return nil
		}
		r.Phase = "WAIT_DRAINED"
		changed = true
	case "WAIT_DRAINED":
		if !c.allAcked("INSTALLED_DRAIN") || !c.allAcked("DRAINED") {
			return nil
		}
		r.Phase = "INSTALL_FINAL"
		if err := c.events(ctx, "INSTALL_FINAL"); err != nil {
			return err
		}
		changed = true
	case "INSTALL_FINAL":
		if !c.allAcked("INSTALLED_FINAL") {
			return nil
		}
		r.Phase = "REOPEN"
		if err := c.events(ctx, "REOPEN"); err != nil {
			return err
		}
		changed = true
	case "REOPEN":
		if !c.allAcked("REOPENED") {
			return nil
		}
		for id, s := range c.state.Reservations {
			if s.Recall == r.ID {
				s.Status = "CLOSED"
				c.state.Reservations[id] = s
			}
		}
		c.state.Fence++
		c.state.Recall = nil
		changed = true
	}
	if changed {
		return c.save()
	}
	return nil
}

func (c *Controller) allAcked(phase string) bool {
	r := c.state.Recall
	for session, event := range r.Events {
		var e struct{ Sequence uint64 }
		if json.Unmarshal(event, &e) != nil {
			return false
		}
		if len(r.Acks[session+"/"+phase+"/"+stringNumber(e.Sequence)]) == 0 {
			return false
		}
	}
	return len(r.Events) > 0
}

func (c *Controller) manifest(scope guestbridge.Context) (json.RawMessage, error) {
	entries := []json.RawMessage{}
	for _, r := range c.state.Reservations {
		if r.Reservation.Scope != scope || r.Status == "CLOSED" {
			continue
		}
		if r.Status != "FROZEN" || r.Recall != c.state.Recall.ID || r.Call == nil {
			return nil, fsapi.Unavailable
		}
		entries = append(entries, r.Entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entryID(entries[i]) < entryID(entries[j]) })
	return canonical(map[string]any{"hash": digest(entries), "entries": entries})
}

func (c *Controller) drained(ctx context.Context) error {
	for _, r := range c.state.Reservations {
		if r.Status == "CLOSED" {
			continue
		}
		if r.Status != "FROZEN" || r.Call == nil || c.state.Recall == nil || r.Recall != c.state.Recall.ID {
			return fsapi.Unavailable
		}
		if err := c.config.Providers[r.Reservation.Scope.Session].CheckFrozenResult(ctx, r.Reservation.Scope, *r.Call); err != nil {
			return err
		}
	}
	return nil
}

func entryID(raw json.RawMessage) string {
	var entry struct{ ID string }
	_ = json.Unmarshal(raw, &entry)
	return entry.ID
}

func manifestHash(raw json.RawMessage) string {
	var manifest struct{ Hash string }
	_ = json.Unmarshal(raw, &manifest)
	return manifest.Hash
}

func stringNumber(n uint64) string { return strconv.FormatUint(n, 10) }

func (c *Controller) closeUnadopted(ctx context.Context, scope guestbridge.Context, ack acknowledgement, raw json.RawMessage) error {
	var manifest struct{ Entries []json.RawMessage }
	if err := json.Unmarshal(ack.Manifest, &manifest); err != nil {
		return err
	}
	included := map[string]bool{}
	for _, entry := range manifest.Entries {
		var claim struct{ Reservation string }
		if err := json.Unmarshal(entry, &claim); err != nil {
			return err
		}
		s, ok := c.state.Reservations[claim.Reservation]
		if !ok || s.Reservation.Scope != scope || s.Status != "FROZEN" || s.Call == nil || s.Recall != ack.Recall || !bytes.Equal(entry, s.Entry) || included[claim.Reservation] {
			return fsapi.Stale
		}
		included[claim.Reservation] = true
	}
	omitted := []guestbridge.DirtyReservation{}
	for _, s := range c.state.Reservations {
		if s.Reservation.Scope != scope || s.Status == "CLOSED" || included[s.Reservation.ID] {
			continue
		}
		if s.Call != nil || (s.Status != "RESERVED" && s.Status != "RESERVING" && s.Status != "CANCELLING") {
			return fsapi.Stale
		}
		omitted = append(omitted, s.Reservation)
	}
	if len(omitted) == 0 {
		return nil
	}
	sort.Slice(omitted, func(i, j int) bool { return omitted[i].ID < omitted[j].ID })
	ledger, err := c.rawState()
	if err != nil {
		return err
	}
	statement := Unadopted{Enrollment: c.config.Authorization.Enrollment, Event: bytes.Clone(c.state.Recall.Events[scope.Session]), Acknowledgement: bytes.Clone(raw), Ledger: ledger, Scope: scope, OldFence: c.state.Fence, Omitted: omitted}
	if err = c.config.Facts.CheckUnadopted(c.factsContext(ctx), statement); err != nil {
		return err
	}
	for _, reservation := range omitted {
		s := c.state.Reservations[reservation.ID]
		if s.ExclusionACK != "" && s.ExclusionACK != hash(raw) {
			return fsapi.Stale
		}
		s.ExclusionACK = hash(raw)
		c.state.Reservations[reservation.ID] = s
		if err = c.closeReservation(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
func (c *Controller) closeReservation(ctx context.Context, s reservationState) error {
	t, ok := c.state.Tickets[s.TicketKey]
	if !ok {
		return fsapi.Stale
	}
	reply, err := c.reply(t, "CANCELLED", nil, nil, nil)
	if err != nil {
		return err
	}
	if !absent(s.ClosureReply) && !bytes.Equal(s.ClosureReply, reply) {
		return fsapi.Stale
	}
	s.Status = "CANCELLING"
	s.ClosureReply = reply
	c.state.Reservations[s.Reservation.ID] = s
	if err = c.save(); err != nil {
		return err
	}
	if err = c.fail("earmark-closing-protected"); err != nil {
		return err
	}
	if _, err = c.config.Providers[s.Reservation.Scope.Session].CloseEarmark(ctx, numberEarmark(s.Reservation), entryID(reply)); err != nil {
		return err
	}
	if err = c.fail("earmark-closed-before-ticket"); err != nil {
		return err
	}
	s.Status = "CLOSED"
	c.state.Reservations[s.Reservation.ID] = s
	if absent(t.Initial) {
		t.Initial, err = c.reply(t, "BLOCKED", nil, nil, nil)
		if err != nil {
			return err
		}
	}
	t.Current = reply
	t.Cancelled = reply
	c.state.Tickets[s.TicketKey] = t
	return c.save()
}
func numberEarmark(r guestbridge.DirtyReservation) controlledvalidation.Earmark {
	return controlledvalidation.Earmark{Scope: r.Scope, Number: r.ClaimNumber, Credentials: r.Credentials, Description: r.Description, Reservation: r.ID, Offset: r.Offset, Length: r.Length}
}

func (c *Controller) finishDeclinedGap(ctx context.Context, key string, t ticketState) error {
	if t.Operation != "DIRTY_RESERVE" || status(t.Initial) != "BLOCKED" {
		return fsapi.Stale
	}
	for _, resource := range c.state.Reservations {
		if resource.TicketKey == key {
			return fsapi.Stale
		}
	}
	var original request
	if err := json.Unmarshal(t.Payload, &original); err != nil {
		return err
	}
	reply, err := c.reply(t, "CANCELLED", nil, nil, nil)
	if err != nil {
		return err
	}
	earmark := controlledvalidation.Earmark{Scope: t.Scope, Number: original.Number, Credentials: t.Request.Credentials, Description: original.Description, Offset: original.Offset, Length: original.Length, Reservation: digest(map[string]any{"scope": t.Scope, "operation": t.Operation, "request_hash": t.PayloadHash, "resource": "declined-number"})}
	if t.DeclinedGap != nil && (!equal(*t.DeclinedGap, earmark) || !bytes.Equal(t.GapClosureReply, reply)) {
		return fsapi.Stale
	}
	t.DeclinedGap = &earmark
	t.GapClosureReply = reply
	c.state.Tickets[key] = t
	if err = c.save(); err != nil {
		return err
	}
	if err = c.fail("declined-gap-protected"); err != nil {
		return err
	}
	if _, err = c.config.Providers[t.Scope.Session].CloseEarmark(ctx, earmark, entryID(reply)); err != nil {
		return err
	}
	if err = c.fail("declined-gap-closed"); err != nil {
		return err
	}
	t.Current = reply
	t.Cancelled = reply
	c.state.Tickets[key] = t
	return c.save()
}
