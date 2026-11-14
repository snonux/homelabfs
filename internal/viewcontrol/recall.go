package viewcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
//
// Every spec-defined outcome is a reply: VIEW_POLL answers EMPTY|EVENT and
// VIEW_ACK answers ACCEPTED|STALE|BLOCKED. An error is returned only where the
// spec defines no reply (protocol/authorization violations, a closed or
// poisoned controller); the endpoint then answers the cell with an explicit
// transport error instead of leaving it silent.
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
	if op == "VIEW_ACK" {
		return c.acknowledge(ctx, scope, raw)
	}
	if _, sealed := c.state.Terminal[scope.Session]; sealed {
		return nil, fsapi.Permission
	}
	// No VIEW_POLL reply exists for a controller that cannot vouch for its
	// events; the explicit error lets the guest's transport call fail promptly.
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return nil, fsapi.Unavailable
	}
	if op == "RESUME" {
		return canonical(map[string]any{"status": "BLOCKED", "ledger_hash": digest(c.state)})
	}
	if op == "VIEW_POLL" {
		return c.poll(ctx, scope, raw)
	}
	return nil, fsapi.Unsupported
}

// poll answers one nonblocking VIEW_POLL. The spec requires every poll to be
// answered with {status:EMPTY|EVENT}; it never parks or leaves a cell silent.
//
// A domain becomes part of the recall inventory only through its first
// VIEW_ACQUIRE. The guest kernel nevertheless starts polling as soon as its
// view control exists, before that acquire (an empty unbound domain is already
// polling so its own first release can recall it). Such an unregistered domain
// has no event to receive and no lease to stop, so it gets EMPTY once its
// capability passes the same kind/scope checks a registered domain implies.
// Answering EMPTY changes no state: registering the domain here would add an
// idle domain to every later inventory_hash and require its ACKs, so the
// inventory remains exactly the acquired domains. The backend reposts the
// poll (same logical VIEW_WAIT continuation or a new VIEW_WAIT), and the poll
// after a later ACQUIRE/RELEASE delivers the RECALL event.
func (c *Controller) poll(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Domain guestbridge.Capability
		After  uint64 `json:"after_sequence"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	domain, registered := c.state.Domains[scope.Session]
	if !registered {
		// The structural checks of the bridge's validateDomain: a DOMAIN
		// capability (no description) for this exact session scope. Its grant
		// and release state live only in the bridge, whose validateDomain runs
		// on every dispatch before Exchange.
		if p.Domain.Token == "" || p.Domain.Kind != "DOMAIN" || p.Domain.Description != nil || p.Domain.Scope != scope {
			return nil, fsapi.Permission
		}
		return emptyPoll()
	}
	// A registered domain must present exactly the capability it acquired with.
	if !equal(domain.Domain, p.Domain) {
		return nil, fsapi.Permission
	}
	// progress only advances an already protected recall. events() builds every
	// event before committing anything and the phase moves only afterwards, so
	// a failed step leaves the ledger unchanged. Contention is therefore
	// answered from the current state and the next poll or ACK retries it:
	// capture refuses a full service queue with NoSpace (fsservice.Controlled),
	// lock contention is WouldBlock and a busy provider Busy. Any other failure
	// has no poll reply and gets a definitive error. (A NoSpace that is really
	// a permanent bound only stalls the recall; the poll is still answered.)
	// A failed protected write
	// poisons the controller; its memory is then not the protected ledger, so
	// that is never answered from, whatever the class.
	if err := c.progress(ctx); err != nil && (c.poisoned.Load() || !transientProgress(err)) {
		return nil, guestbridge.Final(err)
	}
	var event json.RawMessage
	if c.state.Recall != nil {
		event = c.state.Recall.Events[scope.Session]
	}
	if len(event) == 0 {
		return emptyPoll()
	}
	var e struct{ Sequence uint64 }
	if err := json.Unmarshal(event, &e); err != nil {
		return nil, err
	}
	if p.After >= e.Sequence {
		return emptyPoll()
	}
	return canonical(map[string]any{"status": "EVENT", "event": event})
}

// transientProgress reports contention that clears without changing state.
func transientProgress(err error) bool {
	return errors.Is(err, fsapi.WouldBlock) || errors.Is(err, fsapi.NoSpace) || errors.Is(err, fsapi.Busy)
}

// emptyPoll is the canonical EMPTY reply: {"event":null,"status":"EMPTY"}.
func emptyPoll() (json.RawMessage, error) {
	return canonical(map[string]any{"status": "EMPTY", "event": nil})
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

// startRecall starts the all-domain recall: every live inventoried domain
// stops, drains and reinstalls, and admission is closed for every domain
// until REOPEN. Dirty, exclusive, conflicting-admission and expected-target
// recalls use it. It is a no-op while any recall exists.
func (c *Controller) startRecall(ctx context.Context) error {
	return c.beginRecall(ctx, "")
}

// beginRecall protects a new recall over all domains (scope "") or over the
// single domain scope (startDomainRecall). The inventory and closure hashes
// bind exactly the participants, so an ACK can never be replayed across a
// scoped and an all-domain recall. For scope "" both hashes are computed
// exactly as before scoped recalls existed.
func (c *Controller) beginRecall(ctx context.Context, scope string) error {
	if c.state.Recall != nil {
		return nil
	}
	if c.fenceExhausted(scope) || len(c.state.AckArchive)+8*len(c.config.Authorization.Enrollment.Domains) > c.config.Authorization.Enrollment.Bounds.Identities*8 {
		return fsapi.NoSpace
	}
	id, err := c.newUUID()
	if err != nil {
		return err
	}
	c.state.Recall = &recallState{ID: id, Phase: "RECALL", Domain: scope, Events: map[string]json.RawMessage{}, Acks: map[string]json.RawMessage{}, Manifests: map[string]json.RawMessage{}}
	c.state.Recall.Inventory, c.state.Recall.Closure = c.recallHashes(id, scope)
	if err = c.events(ctx, "RECALL"); err != nil {
		c.state.Recall = nil // nothing else was committed by the failed events
		return err
	}
	return c.save()
}

// events issues the next event of phase to every live inventoried domain. It
// builds all events first and commits domain sequences and recall events only
// when every one succeeded, so a failure (capture contention, bound or
// sequence exhaustion) leaves the in-memory ledger equal to the protected one.
func (c *Controller) events(ctx context.Context, phase string) error {
	recall := c.state.Recall
	source, err := c.capture(ctx)
	if err != nil {
		return err
	}
	domains := map[string]domainState{}
	issued := map[string]json.RawMessage{}
	for session, domain := range c.state.Domains {
		// A scoped recall issues events to its one domain only; every other
		// domain keeps its view, leases, sequence and fence untouched.
		if _, sealed := c.state.Terminal[session]; sealed || !recall.covers(session) {
			continue
		}
		if domain.Sequence == ^uint64(0) {
			return fsapi.NoSpace
		}
		domain.Sequence++
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
		domains[session], issued[session] = domain, raw
	}
	if err = c.remember(source); err != nil {
		return err
	}
	for session, domain := range domains {
		c.state.Domains[session] = domain
		recall.Events[session] = issued[session]
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

// progress advances the protected recall by at most one phase and saves it.
// Every phase waits for the exact ACKs of the recall's participants only
// (allAcked iterates the issued events), so a scoped recall never waits for
// a domain outside it.
func (c *Controller) progress(ctx context.Context) error {
	r := c.state.Recall
	if r == nil {
		return nil
	}
	changed, err := c.advancePhase(ctx, r)
	if err != nil || !changed {
		return err
	}
	return c.save()
}

// advancePhase performs one phase transition when its ACKs are complete and
// reports whether the ledger changed. A failed event issue leaves the phase
// unchanged because events() commits nothing on failure; an undrained
// reservation keeps DRAIN waiting without an error.
func (c *Controller) advancePhase(ctx context.Context, r *recallState) (bool, error) {
	switch r.Phase {
	case "RECALL":
		if !c.allAcked("STOPPED") {
			return false, nil
		}
		c.quiesceParticipants(r)
		r.Phase = "DRAIN"
		return true, nil
	case "DRAIN":
		if !c.allAcked("STOPPED") || c.drained(ctx) != nil {
			return false, nil
		}
		return c.issuePhase(ctx, r, "INSTALL_DRAIN")
	case "INSTALL_DRAIN":
		if !c.allAcked("INSTALLED_DRAIN") {
			return false, nil
		}
		r.Phase = "WAIT_DRAINED"
		return true, nil
	case "WAIT_DRAINED":
		if !c.allAcked("INSTALLED_DRAIN") || !c.allAcked("DRAINED") {
			return false, nil
		}
		return c.issuePhase(ctx, r, "INSTALL_FINAL")
	case "INSTALL_FINAL":
		if !c.allAcked("INSTALLED_FINAL") {
			return false, nil
		}
		return c.issuePhase(ctx, r, "REOPEN")
	case "REOPEN":
		if !c.allAcked("REOPENED") {
			return false, nil
		}
		c.finishRecall(r)
		return true, nil
	}
	return false, nil
}

// issuePhase issues phase's events to every participant and enters phase.
func (c *Controller) issuePhase(ctx context.Context, r *recallState, phase string) (bool, error) {
	if err := c.events(ctx, phase); err != nil {
		return false, err // phase unchanged: events committed nothing
	}
	r.Phase = phase
	return true, nil
}

// quiesceParticipants deactivates the leases of every participant once all
// STOPPED ACKs are in: their physical rights are quiesced. Frozen drain
// provenance remains retained. A scoped recall leaves every other domain's
// leases active, because only the participant stopped.
func (c *Controller) quiesceParticipants(r *recallState) {
	for id, l := range c.state.Leases {
		if r.covers(l.Lease.Scope.Session) {
			l.Active = false
			c.state.Leases[id] = l
		}
	}
}

// finishRecall closes the drained reservations of the recall, advances the
// participants' fences and removes the recall. Each participant's guest mount
// advanced its own fence when its REOPENED ACK was accepted; advanceFences
// mirrors exactly those domains.
func (c *Controller) finishRecall(r *recallState) {
	for id, s := range c.state.Reservations {
		if s.Recall == r.ID {
			s.Status = "CLOSED"
			c.state.Reservations[id] = s
		}
	}
	c.advanceFences(r)
	c.state.Recall = nil
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

// drained reports whether every open reservation of the recall's
// participants is frozen in this recall and its drain result is protected.
// A scoped recall starts only when its domain has no open reservation
// (releaseRecallScope) and DIRTY_RESERVE stays closed during any recall, so
// it has nothing to drain; another domain's open reservations are not its
// obligation and must not hold it in DRAIN.
func (c *Controller) drained(ctx context.Context) error {
	for _, r := range c.state.Reservations {
		if r.Status == "CLOSED" || (c.state.Recall != nil && !c.state.Recall.covers(r.Reservation.Scope.Session)) {
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
	statement := Unadopted{Enrollment: c.config.Authorization.Enrollment, Event: bytes.Clone(c.state.Recall.Events[scope.Session]), Acknowledgement: bytes.Clone(raw), Ledger: ledger, Scope: scope, OldFence: c.fence(scope.Session), Omitted: omitted}
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
