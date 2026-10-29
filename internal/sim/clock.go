package sim

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/transport"
)

// Clock supplies owner timestamps and virtual deadline notifications. It starts
// at a fixed nonzero UTC epoch; only the controlled scenario advances it.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	changed chan struct{}
	timers  map[*timer]bool
	next    uint64
	failed  error
}
type timer struct {
	clock    *Clock
	at       time.Time
	c        chan time.Time
	active   bool
	cancel   func()
	sequence uint64
}
type transportClock struct{ clock *Clock }
type deadlineContext struct {
	context.Context
	parent context.Context
	at     time.Time
	mu     sync.Mutex
	err    error
	done   chan struct{}
}

var _ fsapi.Clock = (*Clock)(nil)
var _ transport.Clock = transportClock{}
var _ transport.Timer = (*timer)(nil)

// NewClock constructs a clock with no running timers.
func NewClock() *Clock {
	return &Clock{now: epoch(), changed: make(chan struct{}), timers: make(map[*timer]bool)}
}

// Now returns the deterministic fsapi timestamp.
func (c *Clock) Now() fsapi.Timestamp {
	t := c.Time()
	return fsapi.Timestamp{Seconds: t.Unix(), Nanoseconds: uint32(t.Nanosecond())}
}

// Time returns the current virtual instant.
func (c *Clock) Time() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

// Transport supplies the same clock through transport's timer interface.
func (c *Clock) Transport() transport.Clock { return transportClock{c} }

// AdvanceTo moves time forward and synchronously expires due deadlines. Timer
// notification is buffered; waking Go goroutines does not itself advance time.
func (c *Clock) AdvanceTo(at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed != nil {
		return c.failed
	}
	if at.Before(c.now) {
		return fmt.Errorf("virtual time cannot move backwards")
	}
	c.now = at
	due := make([]*timer, 0, len(c.timers))
	for t := range c.timers {
		if !t.at.After(at) {
			due = append(due, t)
		}
	}
	slices.SortFunc(due, func(a, b *timer) int {
		if order := a.at.Compare(b.at); order != 0 {
			return order
		}
		if a.sequence < b.sequence {
			return -1
		}
		if a.sequence > b.sequence {
			return 1
		}
		return 0
	})
	for _, t := range due {
		delete(c.timers, t)
		t.active = false
		if t.cancel != nil {
			t.cancel()
		}
		t.c <- t.at
	}
	close(c.changed)
	c.changed = make(chan struct{})
	return nil
}
func (c *Clock) notification() (time.Time, <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now, c.changed
}
func (c *Clock) newTimer(d time.Duration, cancel func()) *timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.timers) >= 65536 {
		c.failed = fmt.Errorf("simulation timer budget exhausted")
		d = 0
	}
	c.next++
	t := &timer{sequence: c.next, clock: c, at: c.now.Add(d), c: make(chan time.Time, 1), active: d > 0, cancel: cancel}
	if d <= 0 {
		if cancel != nil {
			cancel()
		}
		t.c <- c.now
	} else {
		c.timers[t] = true
		close(c.changed)
		c.changed = make(chan struct{})
	}
	return t
}
func (t *timer) C() <-chan time.Time { return t.c }
func (t *timer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	active := t.active
	t.active = false
	delete(t.clock.timers, t)
	return active
}
func (c transportClock) Now() time.Time                           { return c.clock.Time() }
func (c transportClock) NewTimer(d time.Duration) transport.Timer { return c.clock.newTimer(d, nil) }
func (c transportClock) WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	at := c.Now().Add(d)
	if p, ok := parent.Deadline(); ok && p.Before(at) {
		at = p
		d = at.Sub(c.Now())
	}
	ctx := &deadlineContext{Context: context.WithoutCancel(parent), parent: parent, at: at, done: make(chan struct{})}
	t := c.clock.newTimer(d, func() { ctx.terminate(context.DeadlineExceeded) })
	stopParent := context.AfterFunc(parent, func() { t.Stop(); ctx.terminate(parent.Err()) })
	if parent.Err() != nil {
		t.Stop()
		ctx.terminate(parent.Err())
	}
	return ctx, func() { stopParent(); t.Stop(); ctx.terminate(context.Canceled) }
}
func (c *deadlineContext) Deadline() (time.Time, bool) { return c.at, true }
func (c *deadlineContext) Done() <-chan struct{}       { return c.done }
func (c *deadlineContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil && c.parent.Err() != nil {
		c.err = c.parent.Err()
		close(c.done)
	}
	return c.err
}
func epoch() time.Time { return time.Unix(1700000000, 0).UTC() }

// AwaitTimer observes an armed transport handoff/backoff timer of exactly the
// requested remaining duration. Context deadline timers are excluded, so a
// concurrent dial context cannot masquerade as the intended backoff boundary.
func (c *Clock) AwaitTimer(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return fmt.Errorf("invalid timer observation delay")
	}
	for {
		c.mu.Lock()
		ready := false
		for t := range c.timers {
			if t.cancel == nil && t.at.Sub(c.now) == delay {
				ready = true
				break
			}
		}
		changed := c.changed
		c.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (c *deadlineContext) terminate(cause error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = c.parent.Err()
		if c.err == nil {
			c.err = cause
		}
		close(c.done)
	}
}

func (c *Clock) expiresBy(at time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for t := range c.timers {
		if !t.at.After(at) {
			return true
		}
	}
	return false
}
