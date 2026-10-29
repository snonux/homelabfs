package failpoint

import (
	"context"
	"fmt"
	"sync"
)

type blockWaiter struct {
	ctx   context.Context
	scope uint64
	hold  *BlockHold
}

// BlockHold is a test-control-only suspension of one live Block waiter. While
// held, cancellation/release cannot let that waiter return from Hit. Release
// the hold before draining its worker. The underlying activation stays armed.
type BlockHold struct {
	activation    *Activation
	waiter        *blockWaiter
	done          chan struct{}
	once          sync.Once
	interrupted   chan struct{}
	interruptOnce sync.Once
}

// Scope assigns subsequent Block entries to a controlled caller invocation.
// Scope changes never relabel existing waiters. Zero means no active caller.
// Normal registries expose no Control and retain the disabled atomic fast path.
func (c *Control) Scope(scope uint64) { c.registry.scope.Store(scope) }

// HoldBlock atomically holds exactly one currently blocked, uncanceled waiter
// in scope. Historical Triggered/Hits alone cannot establish a live checkpoint.
// Released/replaced activations and ambiguous/mismatched waiters fail closed.
func (a *Activation) HoldBlock(scope uint64) (*BlockHold, error) {
	a.blockMu.Lock()
	defer a.blockMu.Unlock()
	if a.rule.Action != Block || scope == 0 {
		return nil, fmt.Errorf("invalid block hold scope")
	}
	select {
	case <-a.done:
		return nil, fmt.Errorf("released block activation")
	default:
	}
	var selected *blockWaiter
	for w := range a.waiters {
		if w.scope != scope || w.ctx.Err() != nil || w.hold != nil || selected != nil {
			return nil, fmt.Errorf("block is canceled, mismatched or ambiguous")
		}
		selected = w
	}
	if selected == nil {
		return nil, fmt.Errorf("no live block waiter")
	}
	h := &BlockHold{activation: a, waiter: selected, done: make(chan struct{}), interrupted: make(chan struct{})}
	selected.hold = h
	return h, nil
}

// Interrupted closes when the held waiter has observed cancellation or release,
// while remaining stopped until Release. It is a test scheduling boundary only.
func (h *BlockHold) Interrupted() <-chan struct{} { return h.interrupted }

// Release permits the held waiter to respond to release/cancellation again.
// It is idempotent and does not release the underlying Block activation.
func (h *BlockHold) Release() {
	h.once.Do(func() { h.activation.blockMu.Lock(); h.waiter.hold = nil; close(h.done); h.activation.blockMu.Unlock() })
}

func (a *Activation) runBlock(ctx context.Context, scope uint64) error {
	w := &blockWaiter{ctx: ctx, scope: scope}
	a.blockMu.Lock()
	if a.waiters == nil {
		a.waiters = make(map[*blockWaiter]bool)
	}
	a.waiters[w] = true
	// Observing Triggered must imply this waiter has been installed for HoldBlock.
	a.triggerOnce.Do(func() { close(a.triggered) })
	a.blockMu.Unlock()
	var result error
	select {
	case <-a.done:
	case <-ctx.Done():
		result = ctx.Err()
	}
	for {
		a.blockMu.Lock()
		hold := w.hold
		if hold == nil {
			delete(a.waiters, w)
			a.blockMu.Unlock()
			return result
		}
		hold.interruptOnce.Do(func() { close(hold.interrupted) })
		a.blockMu.Unlock()
		<-hold.done
	}
}
