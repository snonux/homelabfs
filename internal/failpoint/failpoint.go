package failpoint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Action identifies the fault injected when a rule fires.
type Action uint8

const (
	// ReturnError returns the configured error.
	ReturnError Action = iota + 1
	// Crash exits the entire process without running deferred cleanup.
	// Exercise this action only in a disposable subprocess.
	Crash
	// Sleep waits for the configured duration, release, or context cancellation.
	Sleep
	// Block waits for release or context cancellation.
	Block
)

var (
	// ErrUnknownName indicates that a control requested an unregistered hook.
	ErrUnknownName = errors.New("unknown failpoint name")
	// ErrInvalidRule indicates an invalid action or incompatible rule fields.
	ErrInvalidRule = errors.New("invalid failpoint rule")
)

// Rule describes one activation. Nth == 0 fires on every hit; otherwise only
// the Nth hit fires. Re-enabling a name starts a fresh hit count. Concurrent
// hits are ordered by their arrival at the registry, not by goroutine identity.
// Only the field belonging to Action may be set: Err for ReturnError, ExitCode
// (1..255) for Crash, Duration (> 0) for Sleep, and none for Block.
type Rule struct {
	Action   Action
	Nth      uint64
	Err      error
	ExitCode int
	Duration time.Duration
}

// Registry supplies hooks to application code. Its zero value and New are
// disabled and cannot be controlled. A Registry must not be copied after use.
type Registry struct {
	enabled atomic.Bool
	mu      sync.RWMutex
	active  map[Name]*Activation
}

// Control is an explicit test-only capability to configure a Registry. Only
// NewForTesting exposes it; production construction must use New. It has no
// network listener, environment-variable activation, or admin-socket wiring.
type Control struct {
	registry *Registry
}

// Activation tracks one rule installation. Release affects only this
// installation, even if the same name has since been re-enabled. Disabling or
// replacing a rule also releases its sleeping or blocked hits.
type Activation struct {
	rule        Rule
	hits        atomic.Uint64
	triggered   chan struct{}
	done        chan struct{}
	triggerOnce sync.Once
	releaseOnce sync.Once
}

// New creates a permanently disabled registry for normal application use.
func New() *Registry {
	return &Registry{}
}

// NewForTesting creates a registry and its control capability for an explicitly
// enabled test process or simulator. Keep the control separate from data-path
// consumers. A future test-only daemon entrypoint may retain it for admin use.
func NewForTesting() (*Registry, *Control) {
	r := New()
	return r, &Control{registry: r}
}

// Hit executes an enabled rule at name. With no enabled rules this is one atomic
// load, without locking, allocation, or context inspection. Unknown/disabled
// names are no-ops. Enabled hits count even if their context is already canceled;
// a canceled context prevents a selected action from executing.
//
// Control changes serialize with selecting and counting hits. Actions execute
// outside the lock: Disable cannot retract an already selected error or crash.
// Sleep and Block are interrupted by disable/replacement and return nil on
// release, or ctx.Err() on cancellation. Callers must supply a non-nil context.
func (r *Registry) Hit(ctx context.Context, name Name) error {
	if !r.enabled.Load() {
		return nil
	}
	r.mu.RLock()
	a := r.active[name]
	if a == nil {
		r.mu.RUnlock()
		return nil
	}
	hit := a.hits.Add(1)
	r.mu.RUnlock()
	if a.rule.Nth != 0 && hit != a.rule.Nth {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.triggerOnce.Do(func() { close(a.triggered) })
	return a.run(ctx)
}

// Enable validates and installs a rule for a standard name, replacing any prior
// activation. Validation failure leaves the existing activation unchanged.
func (c *Control) Enable(name Name, rule Rule) (*Activation, error) {
	if !known(name) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownName, name)
	}
	if err := rule.validate(); err != nil {
		return nil, err
	}
	a := &Activation{rule: rule, triggered: make(chan struct{}), done: make(chan struct{})}
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		r.active = make(map[Name]*Activation)
	}
	if old := r.active[name]; old != nil {
		old.Release()
	}
	r.active[name] = a
	r.enabled.Store(true)
	return a, nil
}

// Disable removes a rule and releases its waiting hits. It is idempotent for
// known names and reports unknown names as configuration mistakes.
func (c *Control) Disable(name Name) error {
	if !known(name) {
		return fmt.Errorf("%w: %q", ErrUnknownName, name)
	}
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.active[name]; a != nil {
		a.Release()
		delete(r.active, name)
	}
	r.enabled.Store(len(r.active) != 0)
	return nil
}

// DisableAll removes every rule and releases all sleeping or blocked hits.
func (c *Control) DisableAll() {
	r := c.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.active {
		a.Release()
	}
	clear(r.active)
	r.enabled.Store(false)
}

// Hits returns the number of arrivals counted for this installation.
func (a *Activation) Hits() uint64 {
	return a.hits.Load()
}

// Triggered closes when the first selected, non-canceled hit reaches its action.
// It does not imply that the action has completed. An untriggered activation
// never closes this channel, even when disabled; bound waits with a context.
func (a *Activation) Triggered() <-chan struct{} {
	return a.triggered
}

// Release unblocks current and future sleeping/blocked hits for this activation.
// It is safe to call concurrently and repeatedly. It does not disable the rule
// or reset its hit count, and has no effect on ReturnError or Crash.
func (a *Activation) Release() {
	a.releaseOnce.Do(func() { close(a.done) })
}

func (r Rule) validate() error {
	valid := false
	switch r.Action {
	case ReturnError:
		valid = r.Err != nil && r.ExitCode == 0 && r.Duration == 0
	case Crash:
		valid = r.Err == nil && r.ExitCode >= 1 && r.ExitCode <= 255 && r.Duration == 0
	case Sleep:
		valid = r.Err == nil && r.ExitCode == 0 && r.Duration > 0
	case Block:
		valid = r.Err == nil && r.ExitCode == 0 && r.Duration == 0
	}
	if !valid {
		return fmt.Errorf("%w: action %d has missing or incompatible fields", ErrInvalidRule, r.Action)
	}
	return nil
}

func (a *Activation) run(ctx context.Context) error {
	switch a.rule.Action {
	case ReturnError:
		return a.rule.Err
	case Crash:
		os.Exit(a.rule.ExitCode)
	case Sleep:
		timer := time.NewTimer(a.rule.Duration)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-a.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case Block:
		select {
		case <-a.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil // Enable rejects unknown actions; Crash does not return.
}
