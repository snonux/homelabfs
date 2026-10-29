package sim

import (
	"context"
	"errors"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/transport"
	"reflect"
	"testing"
	"time"
)

func TestVirtualContextTerminalCauseStaysFixed(t *testing.T) {
	for _, test := range []struct {
		name                   string
		parent, cancel, expire bool
		want                   error
	}{
		{name: "parent", parent: true, want: context.Canceled},
		{name: "explicit", cancel: true, want: context.Canceled},
		{name: "timeout", expire: true, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewClock()
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			ctx, cancel := c.Transport().WithTimeout(parent, time.Second)
			defer cancel()
			if test.parent {
				cancelParent()
			}
			if test.cancel {
				cancel()
			}
			if test.expire {
				if err := c.AdvanceTo(c.Time().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			if !errors.Is(ctx.Err(), test.want) {
				t.Fatalf("first cause: %v", ctx.Err())
			}
			if err := c.AdvanceTo(c.Time().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			cancelParent()
			cancel()
			if !errors.Is(ctx.Err(), test.want) {
				t.Fatalf("terminal cause changed: %v", ctx.Err())
			}
		})
	}
}
func TestVirtualTimersExpireByDeadlineThenRegistration(t *testing.T) {
	c := NewClock()
	var order []int
	c.newTimer(2*time.Second, func() { order = append(order, 3) })
	c.newTimer(time.Second, func() { order = append(order, 1) })
	c.newTimer(time.Second, func() { order = append(order, 2) })
	if err := c.AdvanceTo(c.Time().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []int{1, 2, 3}) {
		t.Fatal(order)
	}
}

func TestVirtualReadAndHandoffDeadlinesAndPause(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "handoff"}[handoff], func(t *testing.T) {
			r := newRig(t, 15, 2, nil)
			a, b := r.connect(t, 0, 1)
			if handoff {
				done := make(chan error, 1)
				go func() { done <- a.Send(r.ctx, transport.Ping, fsapi.UUID{}, UUID(15, "handoff"), []byte("unconsumed")) }()
				deliver(t, r, "conn/1/0/write/3")
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if err := r.nodes[1].runtime.Clock.AwaitTimer(r.ctx, time.Second); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := r.network.AwaitRead(r.ctx, 1, 1); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.nodes[1].Pause(); err != nil {
				t.Fatal(err)
			}
			before := r.nodes[1].runtime.Clock.Time()
			if err := r.s.Advance(2 * time.Second); err != nil {
				t.Fatal(err)
			}
			if !r.nodes[1].runtime.Clock.Time().Equal(before) {
				t.Fatal("paused actor timer clock advanced")
			}
			if err := r.nodes[1].Resume(); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Receive(r.ctx); err == nil {
				t.Fatal("elapsed virtual deadline did not close real transport")
			}
		})
	}
}

func TestVirtualContextPropagatesDeadlineCauseToStandardChildren(t *testing.T) {
	c := NewClock()
	parent, cancel := c.Transport().WithTimeout(context.Background(), time.Second)
	defer cancel()
	child, cancelChild := context.WithCancel(parent)
	defer cancelChild()
	if err := c.AdvanceTo(c.Time().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.Done():
	case <-time.After(time.Second):
		t.Fatal("standard child did not observe virtual deadline")
	}
	if !errors.Is(child.Err(), context.DeadlineExceeded) || !errors.Is(context.Cause(parent), context.DeadlineExceeded) {
		t.Fatalf("deadline cause: child=%v parent=%v", child.Err(), context.Cause(parent))
	}
}

func TestDelayedDeliveryCannotRaceAnArmedDeadline(t *testing.T) {
	r := newRig(t, 64, 2, nil)
	a, _ := r.connect(t, 0, 1)
	if err := r.network.SetLink(r.nodes[0].cfg.Address.Addr(), r.nodes[1].cfg.Address.Addr(), Link{Delay: 2 * time.Second, Retry: time.Millisecond, Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() { sent <- a.Send(r.ctx, transport.Ping, fsapi.UUID{}, UUID(64, "late"), nil) }()
	if err := r.s.Step(r.ctx, "conn/1/0/write/3"); err == nil {
		t.Fatal("ambiguous delivery/expiry jump was silently selected")
	}
	if err := r.nodes[0].Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err == nil {
		t.Fatal("abandoned delivery succeeded")
	}
}

func TestVirtualReconnectBackoff(t *testing.T) {
	r := newRig(t, 72, 2, nil)
	// Keep the real registry identity but remove its virtual listener. Reconnect
	// must wait on the injected clock between bounded refused dials.
	if err := r.nodes[1].Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.nodes[0].runtime.Endpoint.Reconnect(r.ctx, r.nodes[1].cfg.Node, r.nodes[1].cfg.Address, transport.Control, transport.Backoff{Initial: 10 * time.Millisecond, Maximum: 20 * time.Millisecond, Attempts: 3})
		done <- err
	}()
	for _, delay := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond} {
		if err := r.nodes[0].runtime.Clock.AwaitTimer(r.ctx, delay); err != nil {
			t.Fatal(err)
		}
		if err := r.s.Advance(delay); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("refused reconnect succeeded")
		}
	case <-r.ctx.Done():
		t.Fatal("backoff used wall time")
	}
}
