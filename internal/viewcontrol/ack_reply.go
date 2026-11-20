package viewcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// ErrNotObserved is what Facts.CheckPhysical/CheckUnadopted return when the
// fixed read-only observer has not yet measured the acknowledged physical
// phase. It wraps fsapi.Permission (the ACK is not authorized yet) but, unlike
// a real authorization refusal, a later measurement can clear it unchanged.
var ErrNotObserved = fmt.Errorf("%w: physical phase not observed yet", fsapi.Permission)

// ErrACKBlockedTooLong answers an ACK that stayed BLOCKED past BlockedACKBound.
var ErrACKBlockedTooLong = fmt.Errorf("%w: VIEW_ACK blocked beyond its bound", fsapi.Unavailable)

// BlockedACKBound limits how long one exact ACK may keep answering BLOCKED.
// The guest kernel resends a BLOCKED ACK every 10 ms without its own limit
// (patch 0004 hfs_mmap_events/hfs_mmap_drained), so an observation that never
// arrives would otherwise livelock. 30 s matches the guest's transport
// deadline; afterwards the ACK gets an error reply and the guest fails.
//
// An early DRAINED (earlyDrained) waits for its peers' INSTALLED_DRAIN, not
// for anything of its own, so its bound runs from the latest point at which
// a peer still owing INSTALLED_DRAIN started its own current BLOCKED wait,
// when that is later than the DRAINED's own first BLOCKED
// (earlyDrainedSince). A peer whose INSTALLED_DRAIN is itself BLOCKED for up
// to the bound and then ACCEPTED therefore never times the early DRAINED out
// first, which would fail the guest's view worker and wedge the recall as in
// zq3-r11. A peer that sends nothing still ends the wait after the bound.
const BlockedACKBound = 30 * time.Second

// errEarlyDrained marks a DRAINED sent before the recall left INSTALL_DRAIN
// (earlyDrained): BLOCKED like WouldBlock, but bounded from its peers' waits.
var errEarlyDrained = fmt.Errorf("%w: DRAINED before every INSTALLED_DRAIN", fsapi.WouldBlock)

// blockedACK remembers when one session's current ACK bytes were first BLOCKED
// and how many BLOCKED replies they got (count feeds only the recall history,
// recordAck). It is in memory only: a refusal never changes the protected
// ledger, and a controller restart starts a fresh bound.
type blockedACK struct {
	hash  string
	since time.Time
	count int
}

// acknowledge answers one VIEW_ACK with the spec reply {status:ACCEPTED|STALE|
// BLOCKED}; c.mu is held. The guest kernel (patch 0004 hfs_mmap_ack) treats
// BLOCKED as -EAGAIN and resends the identical ACK after 10 ms, STALE as
// -ESTALE and fences its view, and a transport error as a failed call.
//
//   - STALE: the ACK cannot acknowledge current work (wrong or finished recall,
//     session, sequence, phase, hashes, manifest or target), or its session
//     is already sealed.
//   - BLOCKED: only classes that clear without changing the ACK: lock or
//     provider contention (WouldBlock, Busy), ErrNotObserved and a DRAINED
//     sent before the recall left INSTALL_DRAIN (earlyDrained,
//     errEarlyDrained), bounded by BlockedACKBound.
//   - error: everything else, including recovery (the disconnect discarded
//     learned ACKs and guest RESUME stays blocked, so it cannot clear for this
//     session), real authorization refusals, bounds, IO and protocol
//     violations. The endpoint answers those with an explicit transport error.
func (c *Controller) acknowledge(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if _, sealed := c.state.Terminal[scope.Session]; sealed {
		return c.ackReply(scope, raw, "STALE")
	}
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return nil, fsapi.Unavailable
	}
	var ack acknowledgement
	if err := json.Unmarshal(raw, &ack); err != nil {
		return nil, guestbridge.Final(fsapi.Invalid)
	}
	checked := c.clock()
	if err := c.ack(ctx, scope, ack, raw); err != nil {
		return c.ackRefusal(scope, raw, err)
	}
	check := c.clock().Sub(checked)
	// An ACK that passed every check is protected before it is answered. A
	// failed write poisons the controller, so ACCEPTED would be uncertain.
	if err := c.save(); err != nil {
		return nil, err
	}
	// Diagnostics only (task ce4): recorded once the ACK is protected and
	// before ackReply clears this session's BLOCKED history.
	c.recordAck(ack, raw, check, c.clock())
	// The ACK is durable now; progress is a separate advancement that the next
	// poll or ACK retries, and a replay of these exact bytes is ACCEPTED.
	_ = c.progress(ctx)
	return c.ackReply(scope, raw, "ACCEPTED")
}

// ackRefusal maps a refused ACK to its spec reply or a definitive error.
func (c *Controller) ackRefusal(scope guestbridge.Context, raw json.RawMessage, err error) (json.RawMessage, error) {
	switch {
	case errors.Is(err, fsapi.Stale):
		return c.ackReply(scope, raw, "STALE")
	case errors.Is(err, errEarlyDrained):
		return c.blockedReply(scope, raw, c.earlyDrainedSince(scope))
	case errors.Is(err, ErrNotObserved), errors.Is(err, fsapi.WouldBlock), errors.Is(err, fsapi.Busy):
		return c.ackReply(scope, raw, "BLOCKED")
	default:
		return nil, guestbridge.Final(err)
	}
}

// ackReply encodes status and maintains the per-session BLOCKED bound: the
// first BLOCKED for exact ACK bytes starts the clock, any other reply or other
// bytes reset it, and BLOCKED past BlockedACKBound becomes an error.
func (c *Controller) ackReply(scope guestbridge.Context, raw json.RawMessage, status string) (json.RawMessage, error) {
	if status != "BLOCKED" {
		delete(c.blockedACKs, scope.Session)
		return canonical(map[string]any{"status": status})
	}
	return c.blockedReply(scope, raw, time.Time{})
}

// blockedReply answers BLOCKED for raw unless it stayed BLOCKED longer than
// BlockedACKBound. The bound runs from the ACK's own first BLOCKED, or from
// peerSince when that is later (an early DRAINED, earlyDrainedSince).
func (c *Controller) blockedReply(scope guestbridge.Context, raw json.RawMessage, peerSince time.Time) (json.RawMessage, error) {
	now := c.clock()
	if c.blockedACKs == nil {
		c.blockedACKs = map[string]blockedACK{}
	}
	current, ok := c.blockedACKs[scope.Session]
	if !ok || current.hash != hash(raw) {
		current = blockedACK{hash: hash(raw), since: now}
	}
	current.count++
	c.blockedACKs[scope.Session] = current
	start := current.since
	if peerSince.After(start) {
		start = peerSince
	}
	if now.Sub(start) > BlockedACKBound {
		delete(c.blockedACKs, scope.Session)
		return nil, guestbridge.Final(ErrACKBlockedTooLong)
	}
	return canonical(map[string]any{"status": "BLOCKED"})
}

// earlyDrainedSince returns the latest first-BLOCKED time of a recall peer
// of scope that still owes its INSTALLED_DRAIN and whose current ACK is
// BLOCKED, or the zero time if there is none. A blockedACKs entry exists
// only while that session's latest ACK reply was BLOCKED.
func (c *Controller) earlyDrainedSince(scope guestbridge.Context) time.Time {
	var latest time.Time
	r := c.state.Recall
	if r == nil {
		return latest
	}
	for session, event := range r.Events {
		var e struct{ Sequence uint64 }
		if session == scope.Session || json.Unmarshal(event, &e) != nil || len(r.Acks[session+"/INSTALLED_DRAIN/"+stringNumber(e.Sequence)]) != 0 {
			continue
		}
		if b, ok := c.blockedACKs[session]; ok && b.since.After(latest) {
			latest = b.since
		}
	}
	return latest
}
