package transport

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Backoff bounds retries. Attempts includes the first immediate attempt. The
// caller must reconcile stream continuity after every successful reconnect.
type Backoff struct {
	Initial, Maximum time.Duration
	Attempts         int
}

// Reconnect repeats full authentication to the same pinned address. It never
// infers retention continuity, changes peer role, or falls back to another host.
func (e *Endpoint) Reconnect(ctx context.Context, peer fsapi.UUID, address netip.AddrPort, class Class, b Backoff) (*Conn, error) {
	if b.Initial <= 0 || b.Maximum < b.Initial || b.Maximum > time.Minute || b.Attempts < 1 || b.Attempts > 100 {
		return nil, fmt.Errorf("invalid bounded reconnect policy")
	}
	delay := b.Initial
	var err error
	for i := 0; i < b.Attempts; i++ {
		var c *Conn
		c, err = e.Dial(ctx, peer, address, class)
		if err == nil {
			return c, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if i+1 == b.Attempts {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-e.done:
			timer.Stop()
			return nil, fmt.Errorf("endpoint closed")
		case <-timer.C:
		}
		delay = min(delay*2, b.Maximum)
	}
	return nil, fmt.Errorf("reconnect exhausted: %w", err)
}
