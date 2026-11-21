package viewcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Stalled recall participants (task 0a4, zq3-r11 R11-2).
//
// A recall waits for the exact ACKs of every participant (allAcked iterates
// the issued events) and has no deadline of its own. A guest session that
// stops acknowledging (its view worker got STALE and stopped for good, its
// hfs_mmap_fail told the host nothing, the guest hung or died) therefore
// held the recall, and with it every other domain's cut-changing admission,
// forever.
//
// The controller now fences such a participant: it drops the session from
// the recall, so the others can install and REOPEN, and refuses every later
// request of that session with ErrSessionFenced (fsapi.Unavailable). It
// does so only when all of the following hold (fenceable):
//
//   - the recall is past RECALL, i.e. every participant's STOPPED was
//     accepted, which the physical Facts certified (the fixed observer
//     measured the guest stopped), and quiesceParticipants deactivated every
//     participant lease; the session's STOPPED manifest is protected;
//   - the session holds no active lease of any kind (no PAGE lease that
//     could authorize a PTE, no DOMAIN/OBJECT lease);
//   - every open reservation of the session is FROZEN in this recall and its
//     drain result is protected (CheckFrozenResult): it has no undrained
//     entry, so no dirty byte is lost and no frozen call is left without
//     its result;
//   - the session owes an ACK of the current phase (it is what the recall
//     waits for) and its latest ACK is not BLOCKED within BlockedACKBound
//     (a guest whose ACK waits for its observation is alive);
//   - the phase has been waiting at least StalledParticipantBound.
//
// A participant missing its STOPPED is never fenced: nothing shows that it
// stopped using its leases. Neither is one with an undrained entry.
//
// Durability. The fence is one protected ledger write (Fenced, the dropped
// event/ACKs/manifest and, when no participant is left, the end of the
// recall), so a crash leaves either no trace or the whole fence; a reopened
// controller reloads Fenced and keeps refusing the session. The fenced
// session's domain fence does not advance: its guest never acknowledged the
// REOPEN, so its stale leases can never be certified released (the runtime
// Facts need the fence advance) and the guest, which fails its view control
// on the transport error, cannot resume on its old view.
//
// The stall clock (phaseClock) is in memory only. It restarts on every phase
// change, a controller reopen, Disconnect and Recover, so a restart only
// delays a fence, never hastens one.

// StalledParticipantBound is how long a recall phase waits for a fenceable
// participant before that participant is fenced: twice the guest's 30 s
// transport window (BlockedACKBound). Within it a slow guest has a full
// BLOCKED window for its ACK plus a full transport deadline to resend it.
const StalledParticipantBound = 2 * BlockedACKBound

// ErrSessionFenced answers every request of a session the controller fenced
// as a stalled recall participant. It wraps fsapi.Unavailable: the bridge
// answers the cell with an explicit transport error and never retries it.
var ErrSessionFenced = fmt.Errorf("%w: session fenced as a stalled recall participant", fsapi.Unavailable)

// fencedSession is the protected record of one fence: the recall and phase
// it was dropped from, its last event sequence and its domain fence then
// (which stays its fence for good).
type fencedSession struct {
	Recall, Phase string
	Sequence      uint64
	Fence         uint64
}

// phaseClock remembers when the current recall phase was first seen by
// this controller (memory only, see above).
type phaseClock struct {
	recall, phase string
	since         time.Time
}

// phaseACKs lists the ACKs each phase waits for, exactly the allAcked
// conditions of advancePhase. RECALL is absent: a participant missing its
// STOPPED is never fenceable.
var phaseACKs = map[string][]string{
	"DRAIN":         {"STOPPED"},
	"INSTALL_DRAIN": {"INSTALLED_DRAIN"},
	"WAIT_DRAINED":  {"INSTALLED_DRAIN", "DRAINED"},
	"INSTALL_FINAL": {"INSTALLED_FINAL"},
	"REOPEN":        {"REOPENED"},
}

// isFenced reports whether session was fenced; c.mu is held.
func (c *Controller) isFenced(session string) bool {
	_, ok := c.state.Fenced[session]
	return ok
}

// checkFenced is the lock-free ingress gate of CheckRequest. Native
// commands belong to no guest domain and are never refused here; the
// locked admission paths check the protected ledger again (isFenced), so a
// request that passed this gate just before a fence is still refused.
func (c *Controller) checkFenced(scope guestbridge.Context, op string) error {
	if op == "NATIVE" {
		return nil
	}
	if _, ok := c.fencedSessions.Load(scope.Session); ok {
		return ErrSessionFenced
	}
	return nil
}

// loadFenced publishes the reopened ledger's fences to the ingress gate.
func (c *Controller) loadFenced() {
	for session := range c.state.Fenced {
		c.fencedSessions.Store(session, true)
	}
}

// markPhase restarts the stall clock for the current recall phase.
func (c *Controller) markPhase() {
	c.phaseClock = phaseClock{}
	if r := c.state.Recall; r != nil {
		c.phaseClock = phaseClock{recall: r.ID, phase: r.Phase, since: c.clock()}
	}
}

// phaseSince returns when the current phase was first seen, starting the
// clock now if it has not seen this phase (after a reopen or a reset).
func (c *Controller) phaseSince() time.Time {
	r := c.state.Recall
	if r == nil {
		return time.Time{}
	}
	if c.phaseClock.recall != r.ID || c.phaseClock.phase != r.Phase || c.phaseClock.since.IsZero() {
		c.markPhase()
	}
	return c.phaseClock.since
}

// owesACK reports whether session lacks an ACK the current phase waits for.
func (c *Controller) owesACK(r *recallState, session string) bool {
	sequence, ok := eventSequence(r, session)
	if !ok {
		return false
	}
	for _, phase := range phaseACKs[r.Phase] {
		if len(r.Acks[session+"/"+phase+"/"+stringNumber(sequence)]) == 0 {
			return true
		}
	}
	return false
}

// fenceable applies every fence condition but the stall time (see the file
// comment). It never changes state.
func (c *Controller) fenceable(ctx context.Context, session string) bool {
	r := c.state.Recall
	if r == nil || phaseACKs[r.Phase] == nil || len(r.Manifests[session]) == 0 || !c.owesACK(r, session) || c.blockedLive(session) {
		return false
	}
	for _, l := range c.state.Leases {
		if l.Active && l.Lease.Scope.Session == session {
			return false
		}
	}
	return c.sessionDrained(ctx, r, session)
}

// sessionDrained reports whether every open reservation of session is
// frozen in recall r with its drain result protected: no undrained entry.
func (c *Controller) sessionDrained(ctx context.Context, r *recallState, session string) bool {
	for _, s := range c.state.Reservations {
		if s.Reservation.Scope.Session != session || s.Status == "CLOSED" {
			continue
		}
		if s.Status != "FROZEN" || s.Call == nil || s.Recall != r.ID {
			return false
		}
		if c.config.Providers[session].CheckFrozenResult(ctx, s.Reservation.Scope, *s.Call) != nil {
			return false
		}
	}
	return true
}

// blockedLive reports whether session's latest ACK is BLOCKED and still
// within BlockedACKBound: the guest is resending it, so it is alive. An
// entry older than the bound belongs to a guest that stopped resending or
// already got the bound's error reply.
func (c *Controller) blockedLive(session string) bool {
	b, ok := c.blockedACKs[session]
	return ok && c.clock().Sub(b.since) <= BlockedACKBound
}

// fenceAt is when the current phase's fenceable participants may be fenced.
func (c *Controller) fenceAt() time.Time {
	return c.phaseSince().Add(StalledParticipantBound)
}

// stalledParticipants returns the participants that may be fenced now,
// sorted, leaving out except (an ACK sender, whose ACK is progress).
func (c *Controller) stalledParticipants(ctx context.Context, except string) []string {
	r := c.state.Recall
	if r == nil || phaseACKs[r.Phase] == nil || c.clock().Before(c.fenceAt()) {
		return nil
	}
	var stalled []string
	for session := range r.Events {
		if session != except && c.fenceable(ctx, session) {
			stalled = append(stalled, session)
		}
	}
	sort.Strings(stalled)
	return stalled
}

// fenceStalled fences every stalled participant (except except) in one
// protected write and reports whether it fenced any. When no participant
// is left the recall ends in the same write (finishRecall advances no
// fence, as no participant reopened), so no ledger ever holds a recall
// without participants. The failpoints mark the crash points: before
// anything changed and after the fence is protected.
func (c *Controller) fenceStalled(ctx context.Context, except string) (bool, error) {
	stalled := c.stalledParticipants(ctx, except)
	if len(stalled) == 0 {
		return false, nil
	}
	if err := c.fail("participant-fence"); err != nil {
		return false, err
	}
	r := c.state.Recall
	now := c.clock()
	for _, session := range stalled {
		c.dropParticipant(r, session)
	}
	ended := len(r.Events) == 0
	if ended {
		c.finishRecall(r)
	}
	if err := c.save(); err != nil {
		return false, err // poisoned: memory is no longer the protected ledger
	}
	for _, session := range stalled {
		c.fencedSessions.Store(session, true)
		c.history.fenced(r.ID, session, r.Phase, now)
	}
	if ended {
		c.history.phase(r.ID, r.Phase, true, now)
	}
	return true, c.fail("participant-fenced-protected")
}

// dropParticipant records session's fence and removes it from recall r:
// its event, learned ACKs and manifest. Its leases are already inactive
// (fenceable), and its drained FROZEN reservations are closed with the
// recall's others by finishRecall. Its AckArchive rows stay: a replay of an
// archived ACK is answered from them, and the session is refused anyway.
func (c *Controller) dropParticipant(r *recallState, session string) {
	sequence, _ := eventSequence(r, session)
	if c.state.Fenced == nil {
		c.state.Fenced = map[string]fencedSession{}
	}
	c.state.Fenced[session] = fencedSession{Recall: r.ID, Phase: r.Phase, Sequence: sequence, Fence: c.fence(session)}
	delete(r.Events, session)
	delete(r.Manifests, session)
	for key := range r.Acks {
		if strings.HasPrefix(key, session+"/") {
			delete(r.Acks, key)
		}
	}
	delete(c.blockedACKs, session)
}

// eventSequence returns the sequence of session's current event in r.
func eventSequence(r *recallState, session string) (uint64, bool) {
	var e struct{ Sequence uint64 }
	raw, ok := r.Events[session]
	if !ok || json.Unmarshal(raw, &e) != nil {
		return 0, false
	}
	return e.Sequence, true
}

// validFenced checks the fences of a reopened ledger: every fenced session
// is a registered domain, holds no active lease, keeps the fence recorded
// when it was fenced and receives no event of the current recall.
func (s ledger) validFenced() bool {
	for session, f := range s.Fenced {
		if _, ok := s.Domains[session]; !ok || DomainFence(s.Fences, session) != f.Fence {
			return false
		}
		if s.Recall != nil {
			if _, ok := s.Recall.Events[session]; ok {
				return false
			}
		}
		for _, l := range s.Leases {
			if l.Active && l.Lease.Scope.Session == session {
				return false
			}
		}
	}
	return true
}
