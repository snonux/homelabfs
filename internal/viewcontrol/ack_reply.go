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
const BlockedACKBound = 30 * time.Second

// blockedACK remembers when one session's current ACK bytes were first BLOCKED.
// It is in memory only: a refusal never changes the protected ledger, and a
// controller restart starts a fresh bound.
type blockedACK struct {
	hash  string
	since time.Time
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
//     provider contention (WouldBlock, Busy) and ErrNotObserved, bounded by
//     BlockedACKBound.
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
	if err := c.ack(ctx, scope, ack, raw); err != nil {
		return c.ackRefusal(scope, raw, err)
	}
	// An ACK that passed every check is protected before it is answered. A
	// failed write poisons the controller, so ACCEPTED would be uncertain.
	if err := c.save(); err != nil {
		return nil, err
	}
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
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	if c.blockedACKs == nil {
		c.blockedACKs = map[string]blockedACK{}
	}
	current, ok := c.blockedACKs[scope.Session]
	if !ok || current.hash != hash(raw) {
		current = blockedACK{hash: hash(raw), since: now}
		c.blockedACKs[scope.Session] = current
	}
	if now.Sub(current.since) > BlockedACKBound {
		delete(c.blockedACKs, scope.Session)
		return nil, guestbridge.Final(ErrACKBlockedTooLong)
	}
	return canonical(map[string]any{"status": status})
}
