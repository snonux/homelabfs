package viewcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Retiring a fenced session (task fh4, follow-up of 0a4).
//
// A participant fence (participant_fence.go) refuses the session for good,
// but left its tickets, families, lineage revisions, leases, reservations
// and AckArchive rows in the ledger, charged against the bounds the
// surviving domains share (Identities, Tickets, ControlTickets, the
// revision and AckArchive caps and LedgerBytes) for the rest of the
// enrollment. RetireFenced releases those charges on an operator's signed
// word. It never unfences: the session stays in Fenced, now with a Retired
// tombstone, so the ingress gate and every locked path keep refusing it
// with ErrSessionFenced, events() never issues it an event and it can
// never be sealed. Nothing of the retired scope can resume on a stale view,
// because nothing of it is ever admitted again.
//
// Why an operator command and not retirement on re-enrollment. The domain
// inventory is part of the signed Enrollment, and the ledger is pinned to
// its EnrollmentHash: a ledger whose hash differs is refused on reopen and
// a ticket of a scope outside the enrollment is refused by validateLedger.
// A guest that re-enrolls (a new session scope, i.e. a new signed
// enrollment) therefore always gets a new controller assembly and a new
// ledger; it can never meet this ledger, so there is no in-ledger event
// on which an automatic retirement could fire. What remains is the
// survivors' capacity in the current enrollment, and only the operator,
// who signed that enrollment, may decide to give it back. The command
// reuses the operator key that signs the Enrollment (Config.Operator).
//
// Authorisation. The operator signs FencedRetirement: the version, this
// controller's EnrollmentHash and the session's exact fence row (recall,
// phase, sequence, fence). Binding the enrollment keeps a signature from
// being replayed into another assembly; binding the row keeps it from
// retiring a different fence of the same session (none can exist, a
// fence is permanent, but the binding costs nothing). The signature is
// kept in the tombstone and verified again on every reopen
// (validRetiredFences) and, independently, by the runtime Facts with the
// same operator key (fenced_retirement_check.go), so a ledger cannot carry
// a retirement the operator did not sign.
//
// Preconditions (fencedRetirable), all under the controller mutex:
//
//   - no recall runs at all. That covers in-flight recall participation
//     (a fenced session has no event, ACK or manifest, but the rows its
//     drained reservations leave are closed only by finishRecall), and it
//     is the same coarse clause VIEW_RETIRE keeps: dropping lease and
//     lineage rows mid-recall has never been analysed (retirementBlocked);
//   - the session holds no active lease of any kind (validFenced already
//     requires that of every fence; checked again);
//   - every reservation of the session is CLOSED: its drain result is
//     protected and no dirty byte or frozen call is open. A fence leaves
//     only drained FROZEN reservations, which finishRecall closes, so once
//     the recall ended this holds; anything else refuses with WouldBlock.
//
// What is released (releaseFencedSession), in the same protected write as
// the tombstone: every ticket row of the session (and its Identities
// charge), the latest retirement receipt of every family it owns, every
// lease and reservation row of it, the revision high-water of every
// lineage of those leases or recorded for the session in LineageFences,
// every AckArchive row of its scope, and then every UUID nothing names any
// more. Kept: the Domains entry (its scope is what the gate, validFenced
// and the Facts check the tombstone against; Domains is bounded by the
// enrollment's domain count, not by a shared bound), its Fences entry (its
// fence stays the recorded one), the Fenced row itself, and each of its
// families reduced to {High, Floor = High, Scope} (defence in depth,
// review of fh4 round 1). Families are not a shared bound; keeping their
// counters keeps ticket numbers monotone even if the tombstone were lost:
// every number the session ever used is at or below the raised floor and
// gets only a rights-free RETIRED_ID, and a new one must be High+1.
//
// Durability. The release and the tombstone are one protected ledger
// write. A crash before it leaves the plain fence, and the same signed
// command retires again; a crash after it leaves the tombstone, and the
// same command is then an idempotent replay (nil). The failpoints mark both
// points.
//
// Schema. The tombstone is a Fenced row, so the ledger keeps
// LedgerSchemaFenced (3) for good and ValidLedgerSchema is unchanged: it
// never returns to schema 2, which a binary before 0a4 would open and
// serve the session from. A 0a4 binary (schema 3, no retirement) refuses
// the retired ledger too, since its validFenced requires the archived
// STOPPED this retirement dropped; and any binary that did accept it
// would still find the session in Fenced and refuse it. No new schema
// number is needed for safety.

// retiredFence is the tombstone of a retired fenced session: the operator's
// signature over its FencedRetirement, verified again on every reopen.
type retiredFence struct {
	Signature []byte
}

// fencedRetirement is the statement that retires the fence row f of session.
func (c *Controller) fencedRetirement(session string, f fencedSession) FencedRetirement {
	return NewFencedRetirement(c.enrollmentHash, session, f.Recall, f.Phase, f.Sequence, f.Fence)
}

// FencedRetirementStatement returns the statement the operator must sign to
// retire session. Only a fenced session has one; asking supplies no
// authority by itself.
func (c *Controller) FencedRetirementStatement(session string) (FencedRetirement, error) {
	if !c.mu.TryLock() {
		return FencedRetirement{}, fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	f, ok := c.state.Fenced[session]
	if !ok {
		return FencedRetirement{}, fsapi.Permission
	}
	return c.fencedRetirement(session, f), nil
}

// RetireFenced retires one fenced session on the operator's signed word and
// releases everything it kept charged (see the file comment). A replay of a
// retirement already protected answers nil. The session stays refused.
func (c *Controller) RetireFenced(ctx context.Context, r FencedRetirement, signature []byte) error {
	if !c.mu.TryLock() {
		return fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	f, err := c.authorizeFencedRetirement(r, signature)
	if err != nil {
		return err
	}
	if f.Retired != nil {
		return nil // replay: already protected
	}
	scope := c.state.Domains[r.Session].Domain.Scope
	if err = c.check(ctx, scope); err != nil {
		return err
	}
	if err = c.fencedRetirable(r.Session); err != nil {
		return err
	}
	if err = c.fail("fenced-retire"); err != nil {
		return err
	}
	c.releaseFencedSession(scope)
	f.Retired = &retiredFence{Signature: bytes.Clone(signature)}
	c.state.Fenced[r.Session] = f
	if err = c.save(); err != nil {
		return err // poisoned: memory is no longer the protected ledger
	}
	return c.fail("fenced-retired-protected")
}

// authorizeFencedRetirement checks the controller state, that r is signed
// by the operator for this enrollment and that it names exactly a fence row
// of this ledger; c.mu is held. The signature is checked before the row,
// so an unsigned statement learns nothing about the fence row. It returns
// the fence row.
func (c *Controller) authorizeFencedRetirement(r FencedRetirement, signature []byte) (fencedSession, error) {
	if c.poisoned.Load() || c.closed.Load() || c.state.Recovery {
		return fencedSession{}, fsapi.Unavailable
	}
	f, ok := c.state.Fenced[r.Session]
	if !ok || r.Version != FencedRetirementVersion || r.EnrollmentHash != c.enrollmentHash || !r.Verify(c.config.Operator, signature) {
		return fencedSession{}, fsapi.Permission
	}
	if r != c.fencedRetirement(r.Session, f) {
		return fencedSession{}, fsapi.Stale
	}
	return f, nil
}

// fencedRetirable refuses with WouldBlock while a recall runs, the session
// holds an active lease or any reservation of it is not CLOSED; c.mu is
// held.
func (c *Controller) fencedRetirable(session string) error {
	if c.state.Recall != nil {
		return fsapi.WouldBlock
	}
	for _, l := range c.state.Leases {
		if l.Active && l.Lease.Scope.Session == session {
			return fsapi.WouldBlock
		}
	}
	for _, s := range c.state.Reservations {
		if s.Reservation.Scope.Session == session && s.Status != "CLOSED" {
			return fsapi.WouldBlock
		}
	}
	return nil
}

// releaseFencedSession drops every row the retired scope kept charged and
// then the UUIDs nothing names any more; c.mu is held and the caller saves.
func (c *Controller) releaseFencedSession(scope guestbridge.Context) {
	families := c.dropSessionTickets(scope.Session)
	c.retireSessionFamilies(scope, families)
	c.dropSessionLeases(scope.Session)
	for key := range c.state.Reservations {
		if c.state.Reservations[key].Reservation.Scope.Session == scope.Session {
			delete(c.state.Reservations, key)
		}
	}
	prefix := digest(scope) + "/"
	for key := range c.state.AckArchive {
		if strings.HasPrefix(key, prefix) {
			delete(c.state.AckArchive, key)
		}
	}
	c.pruneUUIDs()
}

// dropSessionTickets deletes every ticket row of session, returns their
// Identities charge and reports the families those tickets belong to.
func (c *Controller) dropSessionTickets(session string) map[string]bool {
	families := map[string]bool{}
	for key, t := range c.state.Tickets {
		if t.Scope.Session != session {
			continue
		}
		_, fk := ticketKeys(t.Scope, t.Request)
		families[fk] = true
		delete(c.state.Tickets, key)
		c.state.Identities--
	}
	return families
}

// retireSessionFamilies reduces every family scope owns (its recorded
// Scope or one of its tickets) to {High, Floor = High, Scope}: its latest
// retirement receipt is dropped and its floor raised over every number it
// ever admitted, whose rows dropSessionTickets just deleted. The owner is
// recorded so terminalSnapshot of the other sessions still finds it.
func (c *Controller) retireSessionFamilies(scope guestbridge.Context, owned map[string]bool) {
	for key, f := range c.state.Families {
		if !owned[key] && (f.Scope == nil || f.Scope.Session != scope.Session) {
			continue
		}
		if f.Receipt != "" {
			delete(c.state.Retirements, f.Receipt)
		}
		owner := scope
		c.state.Families[key] = family{High: f.High, Floor: f.High, Scope: &owner}
	}
}

// dropSessionLeases deletes every lease row of session (all inactive, see
// fencedRetirable) and the revision high-water of each of their lineages
// and of every lineage LineageFences records for session.
func (c *Controller) dropSessionLeases(session string) {
	for key, l := range c.state.Leases {
		if l.Lease.Scope.Session != session {
			continue
		}
		lineage := lineageID(l.Lease)
		delete(c.state.Revisions, lineage)
		delete(c.state.LineageFences, lineage)
		delete(c.state.Leases, key)
	}
	for lineage, at := range c.state.LineageFences {
		if at.Session == session {
			delete(c.state.Revisions, lineage)
			delete(c.state.LineageFences, lineage)
		}
	}
}

// holdsSessionRows reports whether the ledger still holds any row a
// retirement of scope releases, by the same check the runtime Facts apply
// (RetiredScopeClean).
func (s ledger) holdsSessionRows(scope guestbridge.Context) bool {
	raw, err := json.Marshal(s)
	return err != nil || !RetiredScopeClean(raw, scope)
}

// validRetiredFences verifies the operator signature of every retirement
// tombstone of a reopened ledger against this controller's enrollment.
func (c *Controller) validRetiredFences() bool {
	for session, f := range c.state.Fenced {
		if f.Retired != nil && !c.fencedRetirement(session, f).Verify(c.config.Operator, f.Retired.Signature) {
			return false
		}
	}
	return true
}
