package viewcontrol

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// acknowledge answers one VIEW_ACK with the spec reply {status:ACCEPTED|STALE|
// BLOCKED}; c.mu is held. The guest kernel (patch 0004 hfs_mmap_ack) treats
// BLOCKED as -EAGAIN and resends the identical ACK after 10 ms, STALE as
// -ESTALE and fences its view, and anything but a reply as a failed call.
//
//   - STALE: the ACK cannot acknowledge current work (wrong or finished recall,
//     session, sequence, phase, hashes, manifest or target), or its session
//     is already sealed.
//   - BLOCKED: the ACK may become acceptable unchanged later: the controller
//     is recovering, physical/unadopted facts are not observed yet, drains or
//     frozen results are unresolved, or a provider/bound is busy.
//   - error: only a protocol violation (undecodable payload, unknown phase),
//     a closed/poisoned controller, or an uncertain ledger write. The endpoint
//     answers those with an explicit transport error, never a silent cell.
func (c *Controller) acknowledge(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if _, sealed := c.state.Terminal[scope.Session]; sealed {
		return ackReply("STALE")
	}
	if c.closed.Load() || c.poisoned.Load() {
		return nil, fsapi.Unavailable
	}
	if c.state.Recovery {
		return ackReply("BLOCKED")
	}
	var ack acknowledgement
	if err := json.Unmarshal(raw, &ack); err != nil {
		return nil, guestbridge.Final(fsapi.Invalid)
	}
	if err := c.ack(ctx, scope, ack, raw); err != nil {
		return ackRefusal(err)
	}
	// An ACK that passed every check is protected before it is answered. A
	// failed write poisons the controller, so ACCEPTED would be uncertain.
	if err := c.save(); err != nil {
		return nil, err
	}
	// The ACK is durable now; progress is a separate advancement that the next
	// poll or ACK retries, and a replay of these exact bytes is ACCEPTED.
	_ = c.progress(ctx)
	return ackReply("ACCEPTED")
}

// ackRefusal maps a refused ACK to its spec reply. Stale evidence is STALE;
// classes that can clear without changing the ACK bytes are BLOCKED. Invalid,
// Unsupported and non-semantic errors (undecodable guest manifest bytes) are
// protocol violations and stay errors.
func ackRefusal(err error) (json.RawMessage, error) {
	switch {
	case errors.Is(err, fsapi.Stale):
		return ackReply("STALE")
	case errors.Is(err, fsapi.Unavailable), errors.Is(err, fsapi.WouldBlock), errors.Is(err, fsapi.Busy),
		errors.Is(err, fsapi.NoSpace), errors.Is(err, fsapi.Interrupted), errors.Is(err, fsapi.IO):
		return ackReply("BLOCKED")
	case errors.Is(err, fsapi.Permission):
		// ack() calls Facts only after CheckRequest authorized this exact
		// request; Permission there means the fixed observer has not measured
		// the acknowledged physical phase yet, which a resend can satisfy.
		return ackReply("BLOCKED")
	default:
		return nil, guestbridge.Final(err)
	}
}

func ackReply(status string) (json.RawMessage, error) {
	return canonical(map[string]any{"status": status})
}
