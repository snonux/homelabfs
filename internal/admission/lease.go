package admission

import (
	"fmt"
	"strings"
)

type state uint8

const (
	queued state = iota + 1
	running
	retained
	released
)

// Lease owns a reservation. Callers retain it for the entire resource lifetime;
// a context cancellation, disconnected session, epoch change, RAM receipt or
// timeout has no automatic effect. All methods are safe for concurrent calls.
type Lease struct {
	budget     *Budget
	req        Request
	charge     Resources
	state      state
	ready      bool
	obligation string
	generation uint64
}

// Request returns immutable scopes and original worst-case resource costs.
func (l *Lease) Request() Request { return l.req }

// Accept irrevocably binds the holding to an exact external obligation before
// writable permission or a protocol retention receipt is emitted. A package may
// be accepted while queued. Cancellation then detaches waiting only.
func (l *Lease) Accept(obligation string) error {
	if !validKey(obligation) {
		return ErrInvalid
	}
	b := l.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if l.state == released || (l.obligation != "" && l.obligation != obligation) {
		return ErrState
	}
	if l.obligation == "" {
		l.obligation = strings.Clone(obligation)
		l.generation++
	}
	return nil
}

// SetReady parks/unparks queued work without holding a worker or serial grant.
// Frontends must park conflicting final work before enqueue via Blocked until
// dirty drains and positive domain barriers have completed.
func (l *Lease) SetReady(ready bool) error {
	b := l.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if l.state != queued {
		return ErrState
	}
	l.ready = ready
	l.generation++
	return nil
}

// Finish returns workers and the serial grant, keeping bytes, source space and
// operation credits pinned. Accepted work requires the external publication
// policy's approval before a successor may run. This is not source release.
func (l *Lease) Finish(evidence string) error {
	return l.transition(FinishGrant, evidence)
}

// Release cancels/destroys only unaccepted work. It also removes queued records
// and empty scope keys. Accepted obligations return ErrAccepted unchanged.
func (l *Lease) Release() error {
	b := l.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if l.state == released {
		return ErrState
	}
	if l.obligation != "" {
		return ErrAccepted
	}
	b.release(l)
	return nil
}

// ReleaseAccepted requires verified complete durable equivalents or protected
// handoff authority from Policy. Mutating work must Finish first; releasing
// storage alone can never implicitly open an unfinished serial grant.
func (l *Lease) ReleaseAccepted(evidence string) error {
	return l.transition(ReleaseResources, evidence)
}

// Transfer reserves a separate destination before consulting protected-transfer
// authority, then atomically retires the source locally. Temporary overlap is
// intentional. Failure leaves the source owned and destroys only the unpublished
// destination reservation. Target must be this same process-local Budget; this
// transfers accounting scopes, never bytes over a network. Remote handoff still
// requires the external protocol. A mutating source must Finish before transfer.
func (l *Lease) Transfer(target *Budget, req Request, evidence string) (*Lease, error) {
	if target != l.budget || req.Cost != l.req.Cost || req.Mutating || req.Blocked {
		return nil, ErrInvalid
	}
	source, generation, err := l.authorization(TransferResources, evidence)
	if err != nil {
		return nil, err
	}
	destination, err := target.Reserve(req)
	if err != nil {
		return nil, err
	}
	err = l.budget.cfg.Policy.Authorize(Authorization{Action: TransferResources, Source: source, Target: Holding{destination, req, source.Obligation}, Evidence: evidence})
	if err == nil {
		b := l.budget
		b.mu.Lock()
		if l.generation != generation || l.state == released || destination.state != running || destination.obligation != "" {
			err = ErrState
		} else {
			destination.obligation = source.Obligation
			destination.generation++
			b.release(l)
		}
		b.mu.Unlock()
	}
	if err != nil {
		_ = destination.Release()
		return nil, err
	}
	return destination, nil
}

func (l *Lease) transition(action Action, evidence string) error {
	source, generation, err := l.authorization(action, evidence)
	if err != nil {
		return err
	}
	if source.Obligation != "" {
		if err = l.budget.cfg.Policy.Authorize(Authorization{Action: action, Source: source, Evidence: evidence}); err != nil {
			return fmt.Errorf("protocol resource policy: %w", err)
		}
	}
	b := l.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if l.generation != generation || l.state == released {
		return ErrState
	}
	if action == ReleaseResources {
		b.release(l)
		return nil
	}
	worker := Resources{Workers: l.charge.Workers}
	b.charge(l.req, worker, false)
	l.charge.Workers = 0
	if b.active[l.req.Closure] == l {
		delete(b.active, l.req.Closure)
	}
	l.state = retained
	l.generation++
	return nil
}

func (l *Lease) authorization(action Action, evidence string) (Holding, uint64, error) {
	if len(evidence) > 4096 {
		return Holding{}, 0, ErrInvalid
	}
	b := l.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if l.state == released || (action == FinishGrant && l.state != running) {
		return Holding{}, 0, ErrState
	}
	if action != FinishGrant && (l.obligation == "" || (l.req.Mutating && l.state != retained)) {
		return Holding{}, 0, ErrState
	}
	if l.obligation != "" && b.cfg.Policy == nil {
		return Holding{}, 0, ErrAccepted
	}
	return Holding{l, l.req, l.obligation}, l.generation, nil
}

func (b *Budget) release(l *Lease) {
	if l.state == queued {
		b.unqueue(l)
	}
	if b.active[l.req.Closure] == l {
		delete(b.active, l.req.Closure)
	}
	b.charge(l.req, l.charge, false)
	delete(b.leases, l)
	l.state = released
	l.generation++
}
