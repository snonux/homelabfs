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
// whose view worker stops (it got STALE and stopped for good, or
// hfs_mmap_fail blocked the view and told the host nothing, while the rest
// of the session may still send cleanup traffic; or the guest hung or
// died) therefore
// held the recall, and with it every other domain's cut-changing admission,
// forever.
//
// The controller now fences such a participant: it drops the session from
// the recall, so the others can install and REOPEN, and refuses every later
// request of that session with ErrSessionFenced (fsapi.Unavailable). It
// does so only when all of the following hold (fenceable, silentFor):
//
//   - the recall is past RECALL, i.e. every participant's STOPPED was
//     accepted, which the physical Facts certified (the fixed observer
//     measured the guest stopped), and quiesceParticipants deactivated every
//     participant lease; the session's STOPPED manifest is protected and its
//     STOPPED of this recall is in the protected AckArchive;
//   - the session holds no active lease of any kind (no PAGE lease that
//     could authorize a PTE, no DOMAIN/OBJECT lease);
//   - every open reservation of the session is FROZEN in this recall and its
//     drain result is protected (CheckFrozenResult): it has no undrained
//     entry, so no dirty byte is lost and no frozen call is left without
//     its result;
//   - the session owes an ACK of the current phase: neither learned in the
//     recall nor accepted before in the protected AckArchive (a controller
//     restart discards learned ACKs, and patch 0004 does not resend an
//     accepted one, so a learned-only check fenced live guests, review of
//     0a4 round 1);
//   - the session's view worker sent nothing, no VIEW_POLL (VIEW_WAIT leg)
//     and no VIEW_ACK, for StalledParticipantBound, counted from the later
//     of its last such request and the controller's first sight of the
//     phase (silentSince). A live view worker reposts VIEW_WAIT every 10 ms
//     after EMPTY (hfs_mmap_poll) and resends a BLOCKED ACK every 10 ms, so
//     it is never fenced, however slow its install. Other traffic does not
//     count: a guest whose view worker died (hfs_mmap_fail blocks the view)
//     still sends cleanup, release and retirement requests (patch 0003
//     hfs_session_poll_halt), which would otherwise postpone its fence
//     forever (0a4 review round 2).
//
// The caller of a poll or an ACK is never fenced by its own request, and a
// fence that would leave the recall without participants requires every one
// of them to be silent for the bound (fenceGuard). A participant missing its
// STOPPED, or with an undrained entry, is never fenced.
//
// Durability. The fence is one protected ledger write (Fenced, the ledger
// schema LedgerSchemaFenced, the dropped event/ACKs/manifest and, when no
// participant is left, the end of the recall), so a crash leaves either no
// trace or the whole fence; a reopened controller reloads Fenced and keeps
// refusing the session. The fenced session's domain fence does not advance:
// its guest never acknowledged the REOPEN, so its stale leases can never be
// certified released (the runtime Facts need the fence advance) and the
// guest, which fails its view control on the transport error, cannot resume
// on its old view. A fence is permanent for that enrolled session; there is
// no unfence path. An operator may later retire the fenced session
// (fenced_retirement.go, task fh4), which releases the rows it kept charged
// but keeps the fence row as a tombstone, so the session stays refused.
//
// The clocks (phaseClock, lastSeen, pulses) are in memory only. They restart on a
// controller reopen, Disconnect and Recover, so a restart only delays a
// fence, never hastens one.

// StalledParticipantBound is how long a fenceable participant's view worker
// must be silent before it is fenced: twice the guest's 30 s transport
// window (BlockedACKBound). A live view worker reposts VIEW_WAIT every 10 ms
// after EMPTY, whose native VIEW_POLL legs reach the controller, and
// resends a BLOCKED ACK every 10 ms; a view worker that sent nothing for two
// transport deadlines is not merely waiting for a delayed event delivery or
// a slow install (both show as polls), it stopped. It also caps how long an
// early DRAINED may wait for a fenceable peer (blockedReply). The value is a liveness choice: the safety of a
// fence rests on the conditions above, not on it.
const StalledParticipantBound = 2 * BlockedACKBound

// ErrSessionFenced answers every request of a session the controller fenced
// as a stalled recall participant. It wraps fsapi.Unavailable: the bridge
// answers the cell with an explicit transport error and never retries it.
var ErrSessionFenced = fmt.Errorf("%w: session fenced as a stalled recall participant", fsapi.Unavailable)

// fencedSession is the protected record of one fence: the recall and phase
// it was dropped from, its last event sequence and its domain fence then
// (which stays its fence for good). Retired is the tombstone of an operator
// retirement (fenced_retirement.go, task fh4); omitempty keeps an unretired
// fence row byte-identical to the 0a4 layout.
type fencedSession struct {
	Recall, Phase string
	Sequence      uint64
	Fence         uint64
	Retired       *retiredFence `json:",omitempty"`
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

// pulse counts one view-worker request of session (VIEW_POLL, VIEW_ACK)
// as a sign of life, whatever its answer. It runs at Exchange's entry
// before the controller TryLock, under pulseMu only, so a request that
// meets contention still counts. It reads no clock: the time is taken
// under c.mu when the count is absorbed (absorbPulse), which can only make
// the session look more recently alive, never less.
func (c *Controller) pulse(session string) {
	c.pulseMu.Lock()
	defer c.pulseMu.Unlock()
	if c.pulses == nil {
		c.pulses = map[string]uint64{}
	}
	c.pulses[session]++
}

// absorbPulse turns new pulses of session into lastSeen = now; c.mu is
// held. Exchange runs it for its caller once it holds c.mu, and silentSince
// for every session it measures (catching contended requests).
func (c *Controller) absorbPulse(session string) {
	c.pulseMu.Lock()
	count := c.pulses[session]
	c.pulseMu.Unlock()
	if count == c.absorbed[session] {
		return
	}
	if c.absorbed == nil {
		c.absorbed = map[string]uint64{}
	}
	if c.lastSeen == nil {
		c.lastSeen = map[string]time.Time{}
	}
	c.absorbed[session] = count
	c.lastSeen[session] = c.clock()
}

// markPhase restarts the phase clock for the current recall phase.
func (c *Controller) markPhase() {
	c.phaseClock = phaseClock{}
	if r := c.state.Recall; r != nil {
		c.phaseClock = phaseClock{recall: r.ID, phase: r.Phase, since: c.clock()}
	}
}

// resetLiveness restarts every in-memory liveness clock (Disconnect,
// Recover): no participant is fenced for the time recovery took.
func (c *Controller) resetLiveness() {
	c.phaseClock = phaseClock{}
	c.lastSeen = nil
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

// silentSince is when session's current silence began: its last request or
// the controller's first sight of the phase, whichever is later.
func (c *Controller) silentSince(session string) time.Time {
	c.absorbPulse(session)
	since := c.phaseSince()
	if last := c.lastSeen[session]; last.After(since) {
		since = last
	}
	return since
}

// fenceAt is when session may be fenced if it stays silent.
func (c *Controller) fenceAt(session string) time.Time {
	return c.silentSince(session).Add(StalledParticipantBound)
}

// silentFor reports whether session has been silent for the whole bound.
func (c *Controller) silentFor(session string) bool {
	return !c.clock().Before(c.fenceAt(session))
}

// owesACK reports whether session lacks an ACK the current phase waits for,
// neither learned in the recall nor archived as accepted before. An
// archived ACK counts as delivered: the guest saw it ACCEPTED and will not
// resend it, so a restart's discarded learned ACKs never make a live guest
// look stalled.
func (c *Controller) owesACK(r *recallState, session string) bool {
	sequence, ok := eventSequence(r, session)
	if !ok {
		return false
	}
	scope := c.state.Domains[session].Domain.Scope
	for _, phase := range phaseACKs[r.Phase] {
		learned := len(r.Acks[session+"/"+phase+"/"+stringNumber(sequence)]) != 0
		archived := c.state.AckArchive[archiveKey(scope, acknowledgement{Recall: r.ID, Phase: phase, Sequence: sequence})] != ""
		if !learned && !archived {
			return true
		}
	}
	return false
}

// ArchivedStopped reports whether archive (a ledger's AckArchive) holds an
// accepted STOPPED of scope for recall. The controller and the runtime Facts
// both require it of every fenced session (task 0a4): a fence is allowed
// only after the session's physically certified STOPPED of that recall.
// The session never acknowledges anything again, so pruneAckArchive never
// drops these rows; only an operator's retirement of the fenced session
// does (fenced_retirement.go), and a retired fence is checked without them.
func ArchivedStopped(archive map[string]string, scope guestbridge.Context, recall string) bool {
	prefix := digest(scope) + "/" + recall + "/STOPPED/" // ArchiveKey without the sequence
	for key, value := range archive {
		if value != "" && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// fenceable applies every fence condition but the silence (see the file
// comment). It never changes state.
func (c *Controller) fenceable(ctx context.Context, session string) bool {
	r := c.state.Recall
	if r == nil || phaseACKs[r.Phase] == nil || len(r.Manifests[session]) == 0 || !c.owesACK(r, session) {
		return false
	}
	if !ArchivedStopped(c.state.AckArchive, c.state.Domains[session].Domain.Scope, r.ID) {
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

// stalledParticipants returns the participants that may be fenced now,
// sorted. except (the session whose poll or ACK runs the check) is never
// returned: its request is a sign of life.
func (c *Controller) stalledParticipants(ctx context.Context, except string) []string {
	r := c.state.Recall
	if r == nil || phaseACKs[r.Phase] == nil {
		return nil
	}
	var stalled []string
	for session := range r.Events {
		if session != except && c.silentFor(session) && c.fenceable(ctx, session) {
			stalled = append(stalled, session)
		}
	}
	sort.Strings(stalled)
	if !c.fenceGuard(r, stalled, except) {
		return nil
	}
	return stalled
}

// fenceGuard refuses a fence that would leave the recall without
// participants unless every participant is provably silent: none is the
// caller and each has been silent for the whole bound. stalledParticipants
// already implies this; the guard keeps any later change of its conditions
// from emptying a recall of live guests.
func (c *Controller) fenceGuard(r *recallState, stalled []string, except string) bool {
	if len(stalled) < len(r.Events) {
		return true
	}
	for session := range r.Events {
		if session == except || !c.silentFor(session) {
			return false
		}
	}
	return true
}

// fenceStalled fences every stalled participant (never except) in one
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
// its event, learned ACKs and manifest. The ledger moves to
// LedgerSchemaFenced, which a binary before 0a4 refuses (it would serve the
// session again). Its leases are already inactive (fenceable), and its
// drained FROZEN reservations are closed with the recall's others by
// finishRecall. Its AckArchive rows stay: they prove its STOPPED
// (ArchivedStopped), and the session is refused anyway. An operator
// retirement (fenced_retirement.go) releases them and its other rows later.
func (c *Controller) dropParticipant(r *recallState, session string) {
	sequence, _ := eventSequence(r, session)
	if c.state.Fenced == nil {
		c.state.Fenced = map[string]fencedSession{}
	}
	c.state.Fenced[session] = fencedSession{Recall: r.ID, Phase: r.Phase, Sequence: sequence, Fence: c.fence(session)}
	c.state.Schema = LedgerSchemaFenced
	delete(r.Events, session)
	delete(r.Manifests, session)
	for key := range r.Acks {
		if strings.HasPrefix(key, session+"/") {
			delete(r.Acks, key)
		}
	}
	delete(c.blockedACKs, session)
	delete(c.lastSeen, session)
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
// when it was fenced and receives no event of the current recall. An
// unretired fence has its archived STOPPED of the recall it was fenced in;
// a retired one (task fh4) instead holds no row its retirement released
// (holdsSessionRows), its operator signature being checked by
// validRetiredFences, which needs the controller's operator key.
func (s ledger) validFenced() bool {
	for session, f := range s.Fenced {
		d, ok := s.Domains[session]
		if !ok || DomainFence(s.Fences, session) != f.Fence {
			return false
		}
		if f.Retired == nil && !ArchivedStopped(s.AckArchive, d.Domain.Scope, f.Recall) {
			return false
		}
		if f.Retired != nil && s.holdsSessionRows(d.Domain.Scope) {
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
