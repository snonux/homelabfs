// Package sim supplies bounded, explicitly driven deterministic core fixtures.
// It neither implements replication nor grants normal serving authority.
package sim

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Event is reproducible evidence of one selected event, never a retention claim.
type Event struct {
	Step        uint64
	At          time.Duration
	Key, Detail string
}

// Limits caps retained events, pending events and network bytes. Zero is invalid.
type Limits struct{ Steps, Pending, Bytes int }

type scheduled struct {
	at     time.Time
	detail string
	run    func() error
}

// Scheduler selects events by explicit stable keys. Concurrent callers are not
// supported: a scenario must issue one command at a time and await named core
// boundaries before advancing time. Pending requests may arrive in any order;
// their arrival order never selects a fault, timestamp, or trace position.
type Scheduler struct {
	mu       sync.Mutex
	clock    *Clock
	limits   Limits
	pending  map[string]scheduled
	changed  chan struct{}
	trace    []Event
	failed   error
	actors   []*Node
	networks []*Network
}

// NewScheduler constructs an empty fixture with finite resource budgets.
func NewScheduler(l Limits) (*Scheduler, error) {
	if l.Steps < 1 || l.Steps > 1000000 || l.Pending < 1 || l.Pending > 65536 || l.Bytes < 1 || l.Bytes > 128<<20 {
		return nil, fmt.Errorf("invalid simulation limits")
	}
	return &Scheduler{clock: NewClock(), limits: l, pending: make(map[string]scheduled), changed: make(chan struct{})}, nil
}

// Clock is the shared virtual owner timestamp and deadline source.
func (s *Scheduler) Clock() *Clock { return s.clock }

// Step waits for the exact named boundary, advances to its delivery time, and
// runs it. It never guesses quiescence from an empty queue or goroutine timing.
// ctx bounds a malformed script waiting for an event that can never arrive.
func (s *Scheduler) Step(ctx context.Context, key string) error {
	for {
		s.mu.Lock()
		if s.failed != nil {
			err := s.failed
			s.mu.Unlock()
			return err
		}
		e, ok := s.pending[key]
		if ok {
			delete(s.pending, key)
			if len(s.trace) >= s.limits.Steps {
				s.failed = fmt.Errorf("simulation step budget exhausted")
				s.signal()
				s.mu.Unlock()
				return s.failed
			}
			s.mu.Unlock()
			at := maxTime(s.clock.Time(), e.at)
			if at.After(s.clock.Time()) {
				if err := s.deliveryAdvance(at); err != nil {
					return s.fail(err)
				}
			}
			if err := s.advanceTo(at); err != nil {
				return s.fail(err)
			}
			s.mu.Lock()
			s.trace = append(s.trace, Event{uint64(len(s.trace) + 1), s.clock.Time().Sub(epoch()), key, e.detail})
			s.mu.Unlock()
			if err := e.run(); err != nil {
				return s.fail(err)
			}
			return nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return s.fail(fmt.Errorf("await %s: %w", key, ctx.Err()))
		case <-changed:
		}
	}
}

// Advance explicitly moves virtual time. Call after the scenario has awaited
// the intended boundary; wall time and an empty queue never advance this clock.
func (s *Scheduler) Advance(d time.Duration) error {
	if d < 0 {
		return s.fail(fmt.Errorf("negative time advance"))
	}
	return s.Do("clock", d.String(), func() error { return s.advanceTo(s.clock.Time().Add(d)) })
}

// Do records a synchronous scenario action, including lifecycle and failpoints.
func (s *Scheduler) Do(key, detail string, run func() error) error {
	if len(key) == 0 || len(key) > 256 || len(detail) > 4096 {
		return s.fail(fmt.Errorf("simulation trace text budget"))
	}
	key, detail = strings.Clone(key), strings.Clone(detail)
	s.mu.Lock()
	if s.failed != nil {
		err := s.failed
		s.mu.Unlock()
		return err
	}
	if len(s.trace) >= s.limits.Steps {
		s.mu.Unlock()
		return s.fail(fmt.Errorf("simulation step budget exhausted"))
	}
	s.trace = append(s.trace, Event{uint64(len(s.trace) + 1), s.clock.Time().Sub(epoch()), key, detail})
	s.mu.Unlock()
	if err := run(); err != nil {
		return s.fail(err)
	}
	return nil
}

// Trace returns an owned copy in actual controlled execution order.
func (s *Scheduler) Trace() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.trace...)
}

// Err returns the first failure; exhausted fixtures cannot resume execution.
func (s *Scheduler) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.failed }

func (s *Scheduler) enqueue(key string, e scheduled) error {
	if len(key) == 0 || len(key) > 256 || len(e.detail) > 4096 {
		return s.fail(fmt.Errorf("simulation event text budget"))
	}
	key, e.detail = strings.Clone(key), strings.Clone(e.detail)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed != nil {
		return s.failed
	}
	if _, ok := s.pending[key]; ok {
		return fmt.Errorf("duplicate simulation event %q", key)
	}
	if len(s.pending) >= s.limits.Pending {
		s.failed = fmt.Errorf("simulation pending budget exhausted")
		s.signal()
		return s.failed
	}
	s.pending[key] = e
	s.signal()
	return nil
}
func (s *Scheduler) signal() { close(s.changed); s.changed = make(chan struct{}) }
func (s *Scheduler) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = err
	}
	s.signal()
	return s.failed
}

// Await waits for a named packet to be queued without delivering or advancing it.
// Use it before a crash or an explicit deadline advance at that boundary.
func (s *Scheduler) Await(ctx context.Context, key string) error {
	for {
		s.mu.Lock()
		if s.failed != nil {
			err := s.failed
			s.mu.Unlock()
			return err
		}
		_, ok := s.pending[key]
		changed := s.changed
		s.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return s.fail(fmt.Errorf("await %s: %w", key, ctx.Err()))
		case <-changed:
		}
	}
}
func (s *Scheduler) advanceTo(at time.Time) error {
	if err := s.clock.AdvanceTo(at); err != nil {
		return err
	}
	s.mu.Lock()
	actors := append([]*Node(nil), s.actors...)
	s.mu.Unlock()
	for _, n := range actors {
		n.mu.Lock()
		clock, paused := n.clock, n.paused
		n.mu.Unlock()
		if clock != nil && !paused {
			if err := clock.AdvanceTo(at); err != nil {
				return err
			}
		}
	}
	return nil
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// Pending reports whether Step synchronously requeued a selected event (for
// example retransmission). Call only after Await or Step in the scenario.
func (s *Scheduler) Pending(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.pending[key]
	return ok
}

func (s *Scheduler) deliveryAdvance(at time.Time) error {
	s.mu.Lock()
	actors := append([]*Node(nil), s.actors...)
	networks := append([]*Network(nil), s.networks...)
	s.mu.Unlock()
	if s.clock.expiresBy(at) {
		return fmt.Errorf("delivery crosses an armed timer: explicitly Advance and observe expiration first")
	}
	for _, n := range actors {
		n.mu.Lock()
		clock, paused := n.clock, n.paused
		n.mu.Unlock()
		if clock != nil && !paused && clock.expiresBy(at) {
			return fmt.Errorf("delivery crosses an armed actor timer: explicitly Advance and observe expiration first")
		}
	}
	for _, n := range networks {
		if n.expiresBy(at) {
			return fmt.Errorf("delivery crosses an armed socket deadline: explicitly Advance and observe expiration first")
		}
	}
	return nil
}
