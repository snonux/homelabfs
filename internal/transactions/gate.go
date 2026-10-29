package transactions

import "context"

type gate struct {
	token   chan struct{}
	waiters chan struct{}
}

func newGate() gate {
	g := gate{token: make(chan struct{}, 1), waiters: make(chan struct{}, 64)}
	g.token <- struct{}{}
	return g
}

func (g gate) Lock()   { <-g.token }
func (g gate) Unlock() { g.token <- struct{}{} }
func (g gate) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-g.token:
		return nil
	default:
	}
	select {
	case g.waiters <- struct{}{}:
	default:
		return ErrLimit
	}
	defer func() { <-g.waiters }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.token:
		if err := ctx.Err(); err != nil {
			g.Unlock()
			return err
		}
		return nil
	}
}
